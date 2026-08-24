package mealai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocale"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/mealchat"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

// Chat interprets an initial message or one continuation against replayed,
// deterministically reconstructed state.
func (s *Service) Chat(ctx context.Context, request ChatRequest) (ChatResult, error) {
	locale, err := foodlocale.Parse(request.Locale)
	if err != nil {
		return ChatResult{}, newError(ErrorInvalidInput, err)
	}
	if strings.TrimSpace(request.Message) == "" || !utf8.ValidString(request.Message) || utf8.RuneCountInString(request.Message) > mealchat.MaxMessageRunes {
		return ChatResult{}, newError(ErrorInvalidInput, fmt.Errorf("invalid chat message"))
	}
	if s == nil || s.chatInterpreter == nil {
		return ChatResult{}, newError(ErrorAIUnavailable, fmt.Errorf("chat interpreter is unavailable"))
	}
	var result ChatResult
	if request.State == nil {
		result, err = s.initialChat(ctx, locale.Exact, request.Message)
	} else {
		result, err = s.continueChat(ctx, locale.Exact, request.Message, *request.State)
	}
	if err != nil {
		return ChatResult{}, err
	}
	return finalizeChatResult(locale.Base, result)
}

func (s *Service) initialChat(ctx context.Context, locale, message string) (ChatResult, error) {
	interpretation, err := s.chatInterpreter.InterpretInitial(ctx, message)
	if err != nil {
		return ChatResult{}, mapChatInterpretationError(err)
	}
	inputs := make([]interpretationInput, 0, len(interpretation.Items))
	conversationItems := make([]ConversationItemState, 0, len(interpretation.Items))
	for index, item := range interpretation.Items {
		inputs = append(inputs, interpretationInput{Evidence: item.Evidence, Intent: item.Intent})
		conversationItems = append(conversationItems, ConversationItemState{
			Position: index, Evidence: item.Evidence, Intent: item.Intent,
		})
	}
	state, interpreted, err := s.interpret(ctx, locale, inputs)
	if err != nil {
		return ChatResult{}, err
	}
	items := publicItems(interpreted)
	active := firstUnresolved(items)
	next := ConversationState{
		Version: ConversationVersion, Purpose: interpretation.Purpose,
		Items: conversationItems, ActiveItemIndex: cloneInt(active),
	}
	return ChatResult{
		Purpose: interpretation.Purpose, State: state, Items: items,
		ActiveItemIndex: cloneInt(active), NextState: next,
	}, nil
}

func (s *Service) continueChat(ctx context.Context, locale, message string, state ConversationState) (ChatResult, error) {
	if err := validateConversationState(state); err != nil {
		return ChatResult{}, newError(ErrorInvalidInput, err)
	}
	state = cloneConversationState(state)
	items, err := s.reconstructConversation(ctx, locale, state)
	if err != nil {
		return ChatResult{}, err
	}
	active := firstUnresolved(items)
	if active == nil || state.ActiveItemIndex == nil || *active != *state.ActiveItemIndex {
		return ChatResult{}, newError(ErrorInvalidInput, fmt.Errorf("conversation active item does not match replayed state"))
	}
	item := items[*active]
	continuationRequest, err := chatContinuationRequest(message, item, state.Items[*active])
	if err != nil {
		return ChatResult{}, newError(ErrorResolutionFailure, err)
	}
	decision, err := s.chatInterpreter.InterpretContinuation(ctx, continuationRequest)
	if err != nil {
		return ChatResult{}, mapChatInterpretationError(err)
	}
	decision, err = mealchat.ValidateContinuationDecision(continuationRequest, decision)
	if err != nil {
		return ChatResult{}, newError(ErrorAIInvalidResponse, err)
	}
	if decision.Kind != mealchat.ContinuationUnresolved {
		updated, err := s.applyChatDecision(ctx, locale, item, decision, &state.Items[*active])
		if err != nil {
			return ChatResult{}, err
		}
		items[*active] = updated
	}
	nextActive := firstUnresolved(items)
	state.ActiveItemIndex = cloneInt(nextActive)
	overall := StateReady
	if len(items) == 0 {
		overall = StateEmpty
	} else if nextActive != nil {
		overall = StateClarificationRequired
	}
	return ChatResult{
		Purpose: state.Purpose, State: overall, Items: items,
		ActiveItemIndex: cloneInt(nextActive), NextState: state,
	}, nil
}

func (s *Service) reconstructConversation(ctx context.Context, locale string, state ConversationState) ([]Item, error) {
	inputs := make([]interpretationInput, 0, len(state.Items))
	for _, item := range state.Items {
		inputs = append(inputs, interpretationInput{Evidence: item.Evidence, Intent: item.Intent})
	}
	_, interpreted, err := s.interpret(ctx, locale, inputs)
	if err != nil {
		return nil, err
	}
	items := publicItems(interpreted)
	for index := range items {
		replay := state.Items[index]
		if replay.FoodChoiceID == nil && replay.AmountChoice == nil {
			continue
		}
		current := items[index]
		if current.State != ItemClarificationRequired || current.Clarification == nil {
			return nil, newError(ErrorInvalidInput, fmt.Errorf("conversation choice is not applicable to current state"))
		}
		if replay.FoodChoiceID != nil {
			if current.Clarification.Kind != ClarificationFoodIdentity || !allowedFoodID(current.Clarification, *replay.FoodChoiceID) {
				return nil, newError(ErrorInvalidInput, fmt.Errorf("food choice is outside current candidates"))
			}
			resolved, err := s.ResolveSelection(ctx, ResolveSelectionRequest{
				FoodID: *replay.FoodChoiceID, Locale: locale, Intent: current.Intent,
				Choice: ExplicitChoice{Kind: ChoiceFoodIdentity},
			})
			if err != nil {
				return nil, err
			}
			current = itemFromSelection(current.Mention, resolved)
		} else if current.Clarification.Kind == ClarificationFoodIdentity {
			return nil, newError(ErrorInvalidInput, fmt.Errorf("amount choice has no resolved food identity"))
		}
		if replay.AmountChoice != nil {
			if current.State != ItemClarificationRequired || current.Clarification == nil || current.Clarification.Kind != ClarificationAmount || current.Food == nil {
				return nil, newError(ErrorInvalidInput, fmt.Errorf("amount choice is not applicable to current state"))
			}
			if err := validateReplayAmountChoice(*replay.AmountChoice, current.Clarification); err != nil {
				return nil, newError(ErrorInvalidInput, err)
			}
			resolved, err := s.ResolveSelection(ctx, ResolveSelectionRequest{
				FoodID: current.Food.FoodID, Locale: locale, Intent: current.Intent, Choice: *replay.AmountChoice,
			})
			if err != nil {
				return nil, err
			}
			current = itemFromSelection(current.Mention, resolved)
		}
		items[index] = current
	}
	return items, nil
}

func (s *Service) applyChatDecision(ctx context.Context, locale string, item Item, decision mealchat.ContinuationDecision, replay *ConversationItemState) (Item, error) {
	if item.Clarification == nil {
		return Item{}, newError(ErrorResolutionFailure, fmt.Errorf("active item has no clarification"))
	}
	var foodID int64
	var choice ExplicitChoice
	switch decision.Kind {
	case mealchat.ContinuationFoodRephrase:
		if item.Clarification.Kind != ClarificationFoodIdentity || decision.ReplacementEvidence == nil || decision.ReplacementIntent == nil {
			return Item{}, newError(ErrorAIInvalidResponse, fmt.Errorf("chat returned malformed food rephrase"))
		}
		return s.applyFoodRephrase(ctx, locale, decision, replay)
	case mealchat.ContinuationFoodIdentity:
		if decision.FoodID == nil || item.Clarification.Kind != ClarificationFoodIdentity || !allowedFoodID(item.Clarification, *decision.FoodID) {
			return Item{}, newError(ErrorAIInvalidResponse, fmt.Errorf("chat selected disallowed food identity"))
		}
		foodID = *decision.FoodID
		choice = ExplicitChoice{Kind: ChoiceFoodIdentity}
		replay.FoodChoiceID = cloneInt64(decision.FoodID)
	case mealchat.ContinuationGrams:
		if decision.Grams == nil || item.Clarification.Kind != ClarificationAmount || item.Food == nil {
			return Item{}, newError(ErrorAIInvalidResponse, fmt.Errorf("chat returned malformed grams choice"))
		}
		foodID = item.Food.FoodID
		choice = ExplicitChoice{Kind: ChoiceGrams, Grams: cloneFloat(decision.Grams)}
		replay.AmountChoice = cloneChoice(&choice)
	case mealchat.ContinuationPortion:
		if decision.PortionID == nil || decision.Quantity == nil || item.Clarification.Kind != ClarificationAmount || item.Food == nil || !allowedPortionID(item.Clarification, *decision.PortionID) {
			return Item{}, newError(ErrorAIInvalidResponse, fmt.Errorf("chat selected disallowed portion"))
		}
		foodID = item.Food.FoodID
		choice = ExplicitChoice{Kind: ChoicePortion, PortionID: cloneInt64(decision.PortionID), Quantity: cloneFloat(decision.Quantity)}
		replay.AmountChoice = cloneChoice(&choice)
	default:
		return Item{}, newError(ErrorAIInvalidResponse, fmt.Errorf("unknown chat continuation decision"))
	}
	resolved, err := s.ResolveSelection(ctx, ResolveSelectionRequest{
		FoodID: foodID, Locale: locale, Intent: item.Intent, Choice: choice,
	})
	if err != nil {
		return Item{}, err
	}
	return itemFromSelection(item.Mention, resolved), nil
}

func (s *Service) applyFoodRephrase(ctx context.Context, locale string, decision mealchat.ContinuationDecision, replay *ConversationItemState) (Item, error) {
	evidence := *decision.ReplacementEvidence
	intent := foodintent.FoodIntent{
		Query: decision.ReplacementIntent.Query, Quantity: cloneFloat(decision.ReplacementIntent.Quantity),
		UnitHint: cloneString(decision.ReplacementIntent.UnitHint),
	}
	var amountEvidence *string
	if intent.Quantity == nil && intent.UnitHint == nil {
		if preservedIntent, preservedEvidence, ok := independentlyPreservableAmount(*replay); ok {
			intent.Quantity = cloneFloat(preservedIntent.Quantity)
			intent.UnitHint = cloneString(preservedIntent.UnitHint)
			amountEvidence = cloneString(&preservedEvidence)
		}
	}

	replay.Evidence = evidence
	replay.AmountEvidence = amountEvidence
	replay.Intent = intent
	replay.FoodChoiceID = nil
	replay.AmountChoice = nil

	_, interpreted, err := s.interpret(ctx, locale, []interpretationInput{{Evidence: evidence, Intent: intent}})
	if err != nil {
		return Item{}, err
	}
	if len(interpreted) != 1 {
		return Item{}, newError(ErrorResolutionFailure, fmt.Errorf("food rephrase produced invalid materialized item count"))
	}
	return publicItems(interpreted)[0], nil
}

func validateConversationState(state ConversationState) error {
	if state.Version != ConversationVersion {
		return fmt.Errorf("unsupported conversation version")
	}
	switch state.Purpose {
	case ChatPurposeMealLogging, ChatPurposeNutritionQuery:
	default:
		return fmt.Errorf("conversation purpose cannot be continued")
	}
	if len(state.Items) == 0 || len(state.Items) > foodextraction.MaxItems {
		return fmt.Errorf("invalid conversation item count")
	}
	if state.ActiveItemIndex == nil || *state.ActiveItemIndex < 0 || *state.ActiveItemIndex >= len(state.Items) {
		return fmt.Errorf("invalid active item index")
	}
	for index, item := range state.Items {
		if item.Position != index || strings.TrimSpace(item.Evidence) == "" || !utf8.ValidString(item.Evidence) || utf8.RuneCountInString(item.Evidence) > mealchat.MaxMessageRunes {
			return fmt.Errorf("invalid conversation item ordering or evidence")
		}
		if err := validateContinuationIntent(item.Intent); err != nil {
			return err
		}
		if item.AmountEvidence == nil {
			if err := mealchat.ValidateIntentEvidence(item.Evidence, item.Intent); err != nil {
				return fmt.Errorf("conversation intent evidence is invalid: %w", err)
			}
		} else {
			if _, _, ok := independentlyPreservableAmount(item); !ok {
				return fmt.Errorf("conversation amount evidence is invalid")
			}
		}
		if item.FoodChoiceID != nil && *item.FoodChoiceID <= 0 {
			return fmt.Errorf("invalid conversation food choice")
		}
		if item.AmountChoice != nil {
			if item.AmountChoice.Kind == ChoiceFoodIdentity || validateChoice(1, *item.AmountChoice) != nil {
				return fmt.Errorf("invalid conversation amount choice")
			}
		}
	}
	return nil
}

func validateReplayAmountChoice(choice ExplicitChoice, clarification *Clarification) error {
	if err := validateChoice(1, choice); err != nil || choice.Kind == ChoiceFoodIdentity {
		return fmt.Errorf("malformed replay amount choice")
	}
	if choice.Kind == ChoicePortion && !allowedPortionID(clarification, *choice.PortionID) {
		return fmt.Errorf("portion choice is outside current stored portions")
	}
	return nil
}

func chatContinuationRequest(message string, item Item, replay ConversationItemState) (mealchat.ContinuationRequest, error) {
	if item.Clarification == nil {
		return mealchat.ContinuationRequest{}, fmt.Errorf("active item has no clarification")
	}
	request := mealchat.ContinuationRequest{
		Message: message, OriginalEvidence: replay.Evidence, OriginalAmountEvidence: cloneString(replay.AmountEvidence), OriginalIntent: item.Intent,
	}
	switch item.Clarification.Kind {
	case ClarificationFoodIdentity:
		request.Kind = mealchat.ClarificationFoodIdentity
		request.Candidates = make([]mealchat.FoodCandidate, 0, len(item.Clarification.Candidates))
		request.Portions = []food.Portion{}
		for _, candidate := range item.Clarification.Candidates {
			request.Candidates = append(request.Candidates, mealchat.FoodCandidate{
				FoodID: candidate.FoodID, DisplayName: candidate.DisplayName,
				CanonicalName: candidate.CanonicalName, Brand: candidate.Brand,
			})
		}
	case ClarificationAmount:
		if item.Food == nil {
			return mealchat.ContinuationRequest{}, fmt.Errorf("amount clarification has no food")
		}
		request.Kind = mealchat.ClarificationAmount
		request.Candidates = []mealchat.FoodCandidate{}
		request.Portions = item.Clarification.Portions
		request.ResolvedFood = &mealchat.ResolvedFood{
			FoodID: item.Food.FoodID, DisplayName: item.Food.DisplayName,
			CanonicalName: item.Food.CanonicalName, Brand: item.Food.Brand,
		}
	default:
		return mealchat.ContinuationRequest{}, fmt.Errorf("unknown clarification kind")
	}
	return request, nil
}

func publicItems(interpreted []interpretedItem) []Item {
	items := make([]Item, 0, len(interpreted))
	for _, item := range interpreted {
		items = append(items, Item{
			Mention: item.Evidence, Intent: item.Intent, State: item.State,
			Food: item.Food, Selection: item.Selection, Preview: item.Preview, Clarification: item.Clarification,
		})
	}
	return items
}

func itemFromSelection(evidence string, result ResolveSelectionResult) Item {
	return Item{
		Mention: evidence, Intent: result.Intent, State: result.State,
		Food: result.Food, Selection: result.Selection, Preview: result.Preview, Clarification: result.Clarification,
	}
}

func firstUnresolved(items []Item) *int {
	for index, item := range items {
		if item.State == ItemClarificationRequired {
			return &index
		}
	}
	return nil
}

func allowedFoodID(clarification *Clarification, foodID int64) bool {
	for _, candidate := range clarification.Candidates {
		if candidate.FoodID == foodID {
			return true
		}
	}
	return false
}

func allowedPortionID(clarification *Clarification, portionID int64) bool {
	for _, portion := range clarification.Portions {
		if portion.ID == portionID {
			return true
		}
	}
	return false
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneChoice(choice *ExplicitChoice) *ExplicitChoice {
	if choice == nil {
		return nil
	}
	return &ExplicitChoice{
		Kind: choice.Kind, Grams: cloneFloat(choice.Grams),
		PortionID: cloneInt64(choice.PortionID), Quantity: cloneFloat(choice.Quantity),
	}
}

func cloneConversationState(state ConversationState) ConversationState {
	cloned := ConversationState{
		Version: state.Version, Purpose: state.Purpose,
		Items: make([]ConversationItemState, 0, len(state.Items)), ActiveItemIndex: cloneInt(state.ActiveItemIndex),
	}
	for _, item := range state.Items {
		cloned.Items = append(cloned.Items, ConversationItemState{
			Position: item.Position, Evidence: item.Evidence, AmountEvidence: cloneString(item.AmountEvidence),
			Intent: foodintent.FoodIntent{
				Query: item.Intent.Query, Quantity: cloneFloat(item.Intent.Quantity), UnitHint: cloneString(item.Intent.UnitHint),
			},
			FoodChoiceID: cloneInt64(item.FoodChoiceID), AmountChoice: cloneChoice(item.AmountChoice),
		})
	}
	return cloned
}

func independentlyPreservableAmount(item ConversationItemState) (foodintent.FoodIntent, string, bool) {
	if item.Intent.Quantity == nil || item.Intent.UnitHint == nil {
		return foodintent.FoodIntent{}, "", false
	}
	unit := strings.ToLower(strings.TrimSpace(*item.Intent.UnitHint))
	switch unit {
	case "g", "gr", "gram", "grams", "gramdı", "gramdi":
		unit = "g"
	case "kg", "kilogram":
		unit = "kg"
	case "ml", "mililitre", "millilitre", "milliliter":
		unit = "ml"
	case "l", "lt", "litre", "liter":
		unit = "l"
	default:
		return foodintent.FoodIntent{}, "", false
	}
	evidence := item.Evidence
	if item.AmountEvidence != nil {
		evidence = *item.AmountEvidence
	}
	intent := foodintent.FoodIntent{Query: item.Intent.Query, Quantity: cloneFloat(item.Intent.Quantity), UnitHint: &unit}
	if strings.TrimSpace(evidence) == "" || !utf8.ValidString(evidence) || utf8.RuneCountInString(evidence) > mealchat.MaxMessageRunes || mealchat.ValidateIntentEvidence(evidence, intent) != nil {
		return foodintent.FoodIntent{}, "", false
	}
	return intent, evidence, true
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func mapChatInterpretationError(err error) error {
	switch {
	case mealchat.IsKind(err, mealchat.ErrorInvalidInput):
		return newError(ErrorInvalidInput, err)
	case mealchat.IsKind(err, mealchat.ErrorProviderConfiguration):
		return newError(ErrorAIUnavailable, err)
	case mealchat.IsKind(err, mealchat.ErrorRateLimit):
		return newError(ErrorAIRateLimited, err)
	case mealchat.IsKind(err, mealchat.ErrorTimeout):
		return newError(ErrorAITimeout, err)
	case mealchat.IsKind(err, mealchat.ErrorInvalidProviderOutput):
		return newError(ErrorAIInvalidResponse, err)
	case mealchat.IsKind(err, mealchat.ErrorProviderFailure):
		return newError(ErrorAIFailure, err)
	case mealchat.IsKind(err, mealchat.ErrorCanceled), errors.Is(err, context.Canceled):
		return newError(ErrorCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return newError(ErrorAITimeout, err)
	default:
		return newError(ErrorAIFailure, err)
	}
}
