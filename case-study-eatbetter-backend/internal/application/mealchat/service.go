package mealchat

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

var explicitNumberPattern = regexp.MustCompile(`(?i)(?:^|[^\pL\pN])([0-9]+(?:[.,][0-9]+)?)(?:\s*)(g|gr|gram|grams|gramdı|gramdi)(?:$|[^\pL\pN])`)

// Service validates both sides of the external chat interpretation boundary.
type Service struct{ interpreter Interpreter }

func NewService(interpreter Interpreter) *Service { return &Service{interpreter: interpreter} }

func (s *Service) InterpretInitial(ctx context.Context, message string) (InitialInterpretation, error) {
	if err := validateMessage(message); err != nil {
		return InitialInterpretation{}, NewError(ErrorInvalidInput, err)
	}
	if s == nil || s.interpreter == nil {
		return InitialInterpretation{}, NewError(ErrorProviderConfiguration, fmt.Errorf("chat interpreter is not configured"))
	}
	result, err := s.interpreter.InterpretInitial(ctx, message)
	if err != nil {
		return InitialInterpretation{}, normalizeProviderError(err)
	}
	if err := validateInitial(message, &result); err != nil {
		return InitialInterpretation{}, NewError(ErrorInvalidProviderOutput, err)
	}
	if result.Items == nil {
		result.Items = []InitialItem{}
	}
	return result, nil
}

func (s *Service) InterpretContinuation(ctx context.Context, request ContinuationRequest) (ContinuationDecision, error) {
	if err := validateContinuationRequest(request); err != nil {
		return ContinuationDecision{}, NewError(ErrorInvalidInput, err)
	}
	if s == nil || s.interpreter == nil {
		return ContinuationDecision{}, NewError(ErrorProviderConfiguration, fmt.Errorf("chat interpreter is not configured"))
	}
	decision, err := s.interpreter.InterpretContinuation(ctx, request)
	if err != nil {
		return ContinuationDecision{}, normalizeProviderError(err)
	}
	if err := validateDecision(request, decision); err != nil {
		return ContinuationDecision{}, NewError(ErrorInvalidProviderOutput, err)
	}
	return decision, nil
}

func validateMessage(message string) error {
	if strings.TrimSpace(message) == "" || !utf8.ValidString(message) || utf8.RuneCountInString(message) > MaxMessageRunes {
		return fmt.Errorf("message must contain 1..%d Unicode runes", MaxMessageRunes)
	}
	return nil
}

func validateInitial(source string, result *InitialInterpretation) error {
	switch result.Purpose {
	case PurposeMealLogging, PurposeNutritionQuery, PurposeUnknown:
	default:
		return fmt.Errorf("unknown chat purpose")
	}
	if len(result.Items) > MaxItems {
		return fmt.Errorf("items exceeds maximum of %d", MaxItems)
	}
	if result.Purpose == PurposeUnknown && len(result.Items) != 0 {
		return fmt.Errorf("unknown purpose cannot contain food items")
	}
	searchFrom := 0
	for index := range result.Items {
		item := &result.Items[index]
		if strings.TrimSpace(item.Evidence) == "" {
			return fmt.Errorf("items[%d].evidence is empty", index)
		}
		relative := strings.Index(source[searchFrom:], item.Evidence)
		if relative < 0 {
			return fmt.Errorf("items[%d].evidence is not an available source span in order", index)
		}
		searchFrom += relative + len(item.Evidence)
		item.Intent.Query = strings.TrimSpace(item.Intent.Query)
		if err := validateIntent(item.Intent.Query, item.Intent.Quantity, item.Intent.UnitHint); err != nil {
			return fmt.Errorf("items[%d]: %w", index, err)
		}
		if item.Intent.Quantity != nil && !explicitQuantityEvidence(item.Evidence, *item.Intent.Quantity) {
			return fmt.Errorf("items[%d].intent.quantity has no exact source evidence", index)
		}
		if item.Intent.UnitHint != nil {
			unit := canonicalUnitHint(*item.Intent.UnitHint)
			if !explicitUnitEvidence(item.Evidence, unit, item.Intent.Quantity) {
				return fmt.Errorf("items[%d].intent.unitHint has no exact source evidence", index)
			}
			item.Intent.UnitHint = &unit
		}
	}
	return nil
}

// ValidateInitialInterpretation lets provider adapters reject malformed output
// before it crosses the application boundary. Service validates it again.
func ValidateInitialInterpretation(source string, result *InitialInterpretation) error {
	if err := validateMessage(source); err != nil {
		return err
	}
	return validateInitial(source, result)
}

func validateContinuationRequest(request ContinuationRequest) error {
	if err := validateMessage(request.Message); err != nil {
		return err
	}
	if err := validateIntent(request.OriginalIntent.Query, request.OriginalIntent.Quantity, request.OriginalIntent.UnitHint); err != nil {
		return err
	}
	switch request.Kind {
	case ClarificationFoodIdentity:
		if request.ResolvedFood != nil || len(request.Candidates) == 0 || len(request.Portions) != 0 {
			return fmt.Errorf("malformed food identity context")
		}
		seen := make(map[int64]struct{}, len(request.Candidates))
		for _, candidate := range request.Candidates {
			if candidate.FoodID <= 0 || strings.TrimSpace(candidate.DisplayName) == "" || strings.TrimSpace(candidate.CanonicalName) == "" {
				return fmt.Errorf("invalid food candidate")
			}
			if _, ok := seen[candidate.FoodID]; ok {
				return fmt.Errorf("duplicate food candidate")
			}
			seen[candidate.FoodID] = struct{}{}
		}
	case ClarificationAmount:
		if request.ResolvedFood == nil || request.ResolvedFood.FoodID <= 0 || strings.TrimSpace(request.ResolvedFood.DisplayName) == "" || strings.TrimSpace(request.ResolvedFood.CanonicalName) == "" || len(request.Candidates) != 0 || request.Portions == nil {
			return fmt.Errorf("malformed amount context")
		}
		if request.ResolvedFood.Brand != nil && strings.TrimSpace(*request.ResolvedFood.Brand) == "" {
			return fmt.Errorf("malformed resolved food brand")
		}
		seen := make(map[int64]struct{}, len(request.Portions))
		for _, portion := range request.Portions {
			if portion.ID <= 0 || portion.FoodID != request.ResolvedFood.FoodID || !finitePositive(portion.Amount) || !finitePositive(portion.Grams) || strings.TrimSpace(portion.Measure) == "" {
				return fmt.Errorf("invalid stored portion")
			}
			if _, ok := seen[portion.ID]; ok {
				return fmt.Errorf("duplicate stored portion")
			}
			seen[portion.ID] = struct{}{}
		}
	default:
		return fmt.Errorf("unknown clarification kind")
	}
	return nil
}

func validateDecision(request ContinuationRequest, decision ContinuationDecision) error {
	switch decision.Kind {
	case ContinuationUnresolved:
		if decision.FoodID != nil || decision.Grams != nil || decision.PortionID != nil || decision.Quantity != nil {
			return fmt.Errorf("unresolved decision has choice fields")
		}
	case ContinuationFoodIdentity:
		if request.Kind != ClarificationFoodIdentity || decision.FoodID == nil || decision.Grams != nil || decision.PortionID != nil || decision.Quantity != nil {
			return fmt.Errorf("malformed food identity decision")
		}
		if !containsCandidate(request.Candidates, *decision.FoodID) {
			return fmt.Errorf("food identity is outside the allowed candidate set")
		}
	case ContinuationGrams:
		if request.Kind != ClarificationAmount || decision.FoodID != nil || decision.Grams == nil || decision.PortionID != nil || decision.Quantity != nil || !finitePositive(*decision.Grams) {
			return fmt.Errorf("malformed grams decision")
		}
		if !explicitGramEvidence(request.Message, *decision.Grams) {
			return fmt.Errorf("grams decision has no exact source evidence")
		}
	case ContinuationPortion:
		if request.Kind != ClarificationAmount || decision.FoodID != nil || decision.Grams != nil || decision.PortionID == nil || decision.Quantity == nil || !finitePositive(*decision.Quantity) {
			return fmt.Errorf("malformed portion decision")
		}
		if !containsPortion(request.Portions, *decision.PortionID) {
			return fmt.Errorf("portion is outside the allowed stored set")
		}
		if !explicitQuantityEvidence(request.Message, *decision.Quantity) {
			return fmt.Errorf("portion quantity has no source evidence")
		}
	default:
		return fmt.Errorf("unknown continuation decision")
	}
	return nil
}

// ValidateContinuationDecision applies the same allow-list and explicit
// evidence checks at provider-adapter and application-service boundaries.
func ValidateContinuationDecision(request ContinuationRequest, decision ContinuationDecision) error {
	if err := validateContinuationRequest(request); err != nil {
		return err
	}
	return validateDecision(request, decision)
}

func validateIntent(query string, quantity *float64, unit *string) error {
	trimmed := strings.TrimSpace(query)
	if runes := utf8.RuneCountInString(trimmed); runes < 2 || runes > 120 {
		return fmt.Errorf("intent query must contain 2..120 Unicode runes")
	}
	if quantity != nil && !finitePositive(*quantity) {
		return fmt.Errorf("intent quantity must be finite and positive")
	}
	if unit != nil && (strings.TrimSpace(*unit) == "" || utf8.RuneCountInString(strings.TrimSpace(*unit)) > foodextraction.MaxUnitHintRunes) {
		return fmt.Errorf("invalid intent unit hint")
	}
	return nil
}

func explicitGramEvidence(message string, wanted float64) bool {
	for _, match := range explicitNumberPattern.FindAllStringSubmatch(message, -1) {
		value, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", "."), 64)
		if err == nil && nearlyEqual(value, wanted) {
			return true
		}
	}
	return false
}

func explicitQuantityEvidence(message string, wanted float64) bool {
	normalized := strings.ToLower(message)
	if nearlyEqual(wanted, 1) && (containsWord(normalized, "bir") || containsWord(normalized, "one") || containsWord(normalized, "tek")) {
		return true
	}
	if nearlyEqual(wanted, 0.5) && (containsWord(normalized, "yarım") || containsWord(normalized, "yarim") || containsWord(normalized, "half")) {
		return true
	}
	numberPattern := regexp.MustCompile(`(?:^|[^\pL\pN])([0-9]+(?:[.,][0-9]+)?)(?:$|[^\pL\pN])`)
	for _, match := range numberPattern.FindAllStringSubmatch(message, -1) {
		value, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", "."), 64)
		if err == nil && nearlyEqual(value, wanted) {
			return true
		}
	}
	return false
}

func explicitUnitEvidence(evidence, unit string, quantity *float64) bool {
	lower := strings.ToLower(evidence)
	var words []string
	switch unit {
	case "g":
		words = []string{"g", "gr", "gram", "grams", "gramdı", "gramdi"}
	case "kg":
		words = []string{"kg", "kilogram"}
	case "ml":
		words = []string{"ml", "mililitre", "milliliter", "millilitre"}
	case "l":
		words = []string{"l", "lt", "litre", "liter"}
	case "adet":
		words = []string{"adet", "tane"}
		if quantity != nil && explicitQuantityEvidence(evidence, *quantity) {
			return true
		}
	default:
		words = []string{strings.ToLower(strings.TrimSpace(unit))}
	}
	for _, word := range words {
		if containsWord(lower, word) {
			return true
		}
	}
	return false
}

func canonicalUnitHint(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "gram", "grams", "gr", "g":
		return "g"
	case "kilogram", "kg":
		return "kg"
	case "mililitre", "milliliter", "millilitre", "ml":
		return "ml"
	case "litre", "liter", "lt", "l":
		return "l"
	case "tane", "adet":
		return "adet"
	default:
		return strings.TrimSpace(unit)
	}
}

func containsWord(text, word string) bool {
	for _, token := range strings.FieldsFunc(text, func(r rune) bool { return !unicodeLetterOrNumber(r) }) {
		if token == word {
			return true
		}
	}
	return false
}

func unicodeLetterOrNumber(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || strings.ContainsRune("çğıöşü", r)
}

func containsCandidate(candidates []FoodCandidate, foodID int64) bool {
	for _, candidate := range candidates {
		if candidate.FoodID == foodID {
			return true
		}
	}
	return false
}

func containsPortion(portions []food.Portion, portionID int64) bool {
	for _, portion := range portions {
		if portion.ID == portionID {
			return true
		}
	}
	return false
}

func finitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

func nearlyEqual(left, right float64) bool {
	return math.Abs(left-right) <= 1e-9*math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
}

func normalizeProviderError(err error) error {
	var typed *Error
	if errors.As(err, &typed) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return NewError(ErrorCanceled, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewError(ErrorTimeout, err)
	}
	return NewError(ErrorProviderFailure, err)
}
