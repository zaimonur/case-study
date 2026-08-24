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
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

var (
	numericLiteralPattern   = regexp.MustCompile(`(?:^|[^\pL\pN])([0-9]+(?:[.,][0-9]+)?)`)
	standaloneNumberPattern = regexp.MustCompile(`(?:^|[^\pL\pN])([0-9]+(?:[.,][0-9]+)?)(?:$|[^\pL])`)
	measurementPattern      = regexp.MustCompile(`(?i)(?:^|[^\pL\pN])([0-9]+(?:[.,][0-9]+)?)\s*(kilogram|kg|mililitre|millilitre|milliliter|ml|gramdı|gramdi|grams|gram|gr|g|litre|liter|lt|l)(?:$|[^\pL])`)
)

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
	if err := validateDecision(request, &decision); err != nil {
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
		if err := ValidateIntentEvidence(item.Evidence, item.Intent); err != nil {
			return fmt.Errorf("items[%d]: %w", index, err)
		}
		if item.Intent.UnitHint != nil {
			unit := canonicalUnitHint(*item.Intent.UnitHint)
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
	if strings.TrimSpace(request.OriginalEvidence) != "" {
		if !utf8.ValidString(request.OriginalEvidence) || utf8.RuneCountInString(request.OriginalEvidence) > MaxMessageRunes {
			return fmt.Errorf("invalid original evidence")
		}
		if err := ValidateIntentEvidence(request.OriginalEvidence, request.OriginalIntent); err != nil {
			return fmt.Errorf("original intent evidence: %w", err)
		}
	} else if request.OriginalIntent.Quantity != nil || request.OriginalIntent.UnitHint != nil {
		return fmt.Errorf("original evidence is required for amount intent")
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

func validateDecision(request ContinuationRequest, decision *ContinuationDecision) error {
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
		if request.Kind != ClarificationAmount || decision.FoodID != nil || decision.Grams != nil || decision.PortionID == nil || decision.Quantity != nil && !finitePositive(*decision.Quantity) {
			return fmt.Errorf("malformed portion decision")
		}
		portion := findPortion(request.Portions, *decision.PortionID)
		if portion == nil {
			return fmt.Errorf("portion is outside the allowed stored set")
		}
		if !portionMeasureEvidence(request.Message, portion.Measure) {
			*decision = ContinuationDecision{Kind: ContinuationUnresolved}
			return nil
		}
		effectiveQuantity, ok := effectivePortionQuantity(request, decision.Quantity)
		if !ok {
			*decision = ContinuationDecision{Kind: ContinuationUnresolved}
			return nil
		}
		decision.Quantity = &effectiveQuantity
	default:
		return fmt.Errorf("unknown continuation decision")
	}
	return nil
}

// ValidateContinuationDecision applies the same allow-list and explicit
// evidence checks at provider-adapter and application-service boundaries.
func ValidateContinuationDecision(request ContinuationRequest, decision ContinuationDecision) (ContinuationDecision, error) {
	if err := validateContinuationRequest(request); err != nil {
		return ContinuationDecision{}, err
	}
	if err := validateDecision(request, &decision); err != nil {
		return ContinuationDecision{}, err
	}
	return decision, nil
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
	return measurementEvidence(message, wanted, "g")
}

// ValidateIntentEvidence proves that any quantity and unit in an intent came
// from the item's original evidence. It is reused for initial output and
// untrusted conversation-state replay.
func ValidateIntentEvidence(evidence string, intent foodintent.FoodIntent) error {
	if intent.Quantity == nil {
		if intent.UnitHint != nil && !unitPhraseEvidence(evidence, canonicalUnitHint(*intent.UnitHint)) {
			return fmt.Errorf("intent unit has no exact source evidence")
		}
		return nil
	}
	quantity := *intent.Quantity
	if !finitePositive(quantity) {
		return fmt.Errorf("intent quantity must be finite and positive")
	}
	if intent.UnitHint == nil {
		if !standaloneQuantityEvidence(evidence, quantity) {
			return fmt.Errorf("intent quantity has no exact source evidence")
		}
		return nil
	}
	unit := canonicalUnitHint(*intent.UnitHint)
	var supported bool
	switch unit {
	case "g", "kg", "ml", "l":
		supported = measurementEvidence(evidence, quantity, unit)
	case "adet":
		supported = explicitCountEvidence(evidence, intent.Query, quantity)
	default:
		supported = quantityUnitPhraseEvidence(evidence, quantity, unit)
	}
	if !supported {
		return fmt.Errorf("intent quantity/unit has no exact source evidence")
	}
	return nil
}

func measurementEvidence(evidence string, wanted float64, canonicalUnit string) bool {
	for _, match := range measurementPattern.FindAllStringSubmatch(evidence, -1) {
		value, ok := parseEvidenceNumber(match[1])
		if ok && nearlyEqual(value, wanted) && canonicalUnitHint(match[2]) == canonicalUnit {
			return true
		}
	}
	return wordQuantityUnitPhraseEvidence(evidence, wanted, canonicalUnit)
}

func standaloneQuantityEvidence(evidence string, wanted float64) bool {
	for _, match := range standaloneNumberPattern.FindAllStringSubmatch(evidence, -1) {
		value, ok := parseEvidenceNumber(match[1])
		if ok && nearlyEqual(value, wanted) {
			return true
		}
	}
	return wordQuantityEvidence(evidence, wanted)
}

func explicitCountEvidence(evidence, query string, wanted float64) bool {
	if quantityUnitPhraseEvidence(evidence, wanted, "adet") || quantityUnitPhraseEvidence(evidence, wanted, "tane") {
		return true
	}
	for _, explicitUnit := range []string{"g", "kg", "ml", "l", "dilim", "bardak", "yemek kaşığı", "çay kaşığı"} {
		if quantityUnitPhraseEvidence(evidence, wanted, explicitUnit) {
			return false
		}
	}
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return false
	}
	pattern := regexp.MustCompile(`(?i)(?:^|[^\pL\pN])([0-9]+(?:[.,][0-9]+)?)\s*` + phraseExpression(query) + `(?:$|[^\pL\pN])`)
	for _, match := range pattern.FindAllStringSubmatch(evidence, -1) {
		value, ok := parseEvidenceNumber(match[1])
		if ok && nearlyEqual(value, wanted) {
			return true
		}
	}
	return wordCountEvidence(evidence, query, wanted)
}

func wordCountEvidence(evidence, query string, wanted float64) bool {
	var words []string
	switch {
	case nearlyEqual(wanted, 1):
		words = []string{"bir", "tek", "one"}
	case nearlyEqual(wanted, 0.5):
		words = []string{"yarım", "yarim", "half"}
	default:
		return false
	}
	for _, word := range words {
		for _, following := range append([]string{query}, measureAliases("adet")...) {
			pattern := regexp.MustCompile(`(?i)(?:^|[^\pL])` + phraseExpression(word) + `\s+` + phraseExpression(following) + `(?:$|[^\pL\pN])`)
			if pattern.MatchString(evidence) {
				return true
			}
		}
	}
	return false
}

func quantityUnitPhraseEvidence(evidence string, wanted float64, unit string) bool {
	aliases := measureAliases(unit)
	for _, alias := range aliases {
		pattern := regexp.MustCompile(`(?i)(?:^|[^\pL\pN])([0-9]+(?:[.,][0-9]+)?)\s*` + phraseExpression(alias) + `(?:$|[^\pL])`)
		for _, match := range pattern.FindAllStringSubmatch(evidence, -1) {
			value, ok := parseEvidenceNumber(match[1])
			if ok && nearlyEqual(value, wanted) {
				return true
			}
		}
	}
	return wordQuantityUnitPhraseEvidence(evidence, wanted, unit)
}

func wordQuantityUnitPhraseEvidence(evidence string, wanted float64, unit string) bool {
	var words []string
	switch {
	case nearlyEqual(wanted, 1):
		words = []string{"bir", "tek", "one"}
	case nearlyEqual(wanted, 0.5):
		words = []string{"yarım", "yarim", "half"}
	default:
		return false
	}
	for _, word := range words {
		for _, alias := range measureAliases(unit) {
			pattern := regexp.MustCompile(`(?i)(?:^|[^\pL])` + phraseExpression(word) + `\s+` + phraseExpression(alias) + `(?:$|[^\pL])`)
			if pattern.MatchString(evidence) {
				return true
			}
		}
	}
	return false
}

func effectivePortionQuantity(request ContinuationRequest, providerQuantity *float64) (float64, bool) {
	latest := explicitQuantities(request.Message)
	if len(latest) > 1 {
		return 0, false
	}
	if len(latest) == 1 {
		if providerQuantity != nil && !nearlyEqual(*providerQuantity, latest[0]) {
			return 0, false
		}
		return latest[0], true
	}
	if request.OriginalIntent.Quantity == nil || !reusableOriginalQuantity(request) {
		return 0, false
	}
	original := *request.OriginalIntent.Quantity
	if providerQuantity != nil && !nearlyEqual(*providerQuantity, original) {
		return 0, false
	}
	return original, true
}

func reusableOriginalQuantity(request ContinuationRequest) bool {
	if request.OriginalIntent.Quantity == nil || strings.TrimSpace(request.OriginalEvidence) == "" {
		return false
	}
	if request.OriginalIntent.UnitHint != nil {
		switch canonicalUnitHint(*request.OriginalIntent.UnitHint) {
		case "g", "kg", "ml", "l":
			return false
		}
	}
	return ValidateIntentEvidence(request.OriginalEvidence, request.OriginalIntent) == nil
}

func explicitQuantities(evidence string) []float64 {
	values := make([]float64, 0, 2)
	for _, match := range numericLiteralPattern.FindAllStringSubmatch(evidence, -1) {
		value, ok := parseEvidenceNumber(match[1])
		if ok && finitePositive(value) {
			values = appendDistinct(values, value)
		}
	}
	for _, candidate := range []struct {
		value float64
		words []string
	}{{1, []string{"bir", "tek", "one"}}, {0.5, []string{"yarım", "yarim", "half"}}} {
		for _, word := range candidate.words {
			if containsWord(strings.ToLower(evidence), word) {
				values = appendDistinct(values, candidate.value)
				break
			}
		}
	}
	return values
}

func wordQuantityEvidence(evidence string, wanted float64) bool {
	for _, value := range explicitQuantities(evidence) {
		if nearlyEqual(value, wanted) {
			return true
		}
	}
	return false
}

func appendDistinct(values []float64, candidate float64) []float64 {
	for _, value := range values {
		if nearlyEqual(value, candidate) {
			return values
		}
	}
	return append(values, candidate)
}

func parseEvidenceNumber(raw string) (float64, bool) {
	value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	return value, err == nil && finitePositive(value)
}

func portionMeasureEvidence(message, measure string) bool {
	for _, alias := range measureAliases(measure) {
		if containsPhrase(message, alias) {
			return true
		}
	}
	return false
}

func unitPhraseEvidence(message, unit string) bool {
	for _, alias := range measureAliases(unit) {
		if containsPhrase(message, alias) {
			return true
		}
	}
	return false
}

func measureAliases(measure string) []string {
	normalized := normalizePhrase(measure)
	groups := [][]string{
		{"g", "gr", "gram", "grams", "gramdı", "gramdi"},
		{"kg", "kilogram"},
		{"ml", "mililitre", "millilitre", "milliliter"},
		{"l", "lt", "litre", "liter"},
		{"adet", "tane", "taneydi", "adetti"},
		{"dilim", "slice", "slices"},
		{"bardak", "cup", "cups"},
		{"yemek kaşığı", "yemek kasigi", "tablespoon", "tablespoons"},
		{"çay kaşığı", "cay kasigi", "teaspoon", "teaspoons"},
	}
	for _, group := range groups {
		for _, alias := range group {
			if normalized == alias {
				return group
			}
		}
	}
	if normalized == "" {
		return nil
	}
	return []string{normalized}
}

func containsPhrase(text, phrase string) bool {
	phrase = normalizePhrase(phrase)
	if phrase == "" {
		return false
	}
	pattern := regexp.MustCompile(`(?i)(?:^|[^\pL])` + phraseExpression(phrase) + `(?:$|[^\pL])`)
	return pattern.MatchString(text)
}

func phraseExpression(phrase string) string {
	parts := strings.Fields(normalizePhrase(phrase))
	for index := range parts {
		parts[index] = regexp.QuoteMeta(parts[index])
	}
	return strings.Join(parts, `\s+`)
}

func normalizePhrase(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func canonicalUnitHint(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "gram", "grams", "gramdı", "gramdi", "gr", "g":
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

func findPortion(portions []food.Portion, portionID int64) *food.Portion {
	for index := range portions {
		portion := &portions[index]
		if portion.ID == portionID {
			return portion
		}
	}
	return nil
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
