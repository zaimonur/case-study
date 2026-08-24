package mealai

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

const maxAssistantResponseRunes = 1200

func finalizeChatResult(baseLocale string, result ChatResult) (ChatResult, error) {
	assistant, err := buildAssistantResponse(baseLocale, result)
	if err != nil {
		return ChatResult{}, newError(ErrorResolutionFailure, err)
	}
	result.Assistant = assistant
	if err := ValidateAssistantResponse(result); err != nil {
		return ChatResult{}, newError(ErrorResolutionFailure, err)
	}
	return result, nil
}

func buildAssistantResponse(baseLocale string, result ChatResult) (AssistantResponse, error) {
	turkish := baseLocale == "tr"
	switch result.State {
	case StateReady:
		if result.ActiveItemIndex != nil || len(result.Items) == 0 {
			return AssistantResponse{}, fmt.Errorf("malformed ready assistant context")
		}
		switch result.Purpose {
		case ChatPurposeNutritionQuery:
			return AssistantResponse{Kind: AssistantNutritionAnswer, Text: nutritionAnswerText(result.Items, turkish)}, nil
		case ChatPurposeMealLogging:
			if turkish {
				return AssistantResponse{Kind: AssistantMealReady, Text: "Tamam, öğünü hazırladım. Kaydetmeden önce değerleri kontrol edebilirsin."}, nil
			}
			return AssistantResponse{Kind: AssistantMealReady, Text: "Your meal is ready. You can review the values before saving it."}, nil
		default:
			return AssistantResponse{}, fmt.Errorf("ready state has unsupported purpose")
		}
	case StateClarificationRequired:
		if result.ActiveItemIndex == nil || *result.ActiveItemIndex < 0 || *result.ActiveItemIndex >= len(result.Items) {
			return AssistantResponse{}, fmt.Errorf("invalid active clarification context")
		}
		active := result.Items[*result.ActiveItemIndex]
		if active.State != ItemClarificationRequired || active.Clarification == nil {
			return AssistantResponse{}, fmt.Errorf("active item has no clarification")
		}
		text, err := clarificationText(active, turkish)
		if err != nil {
			return AssistantResponse{}, err
		}
		return AssistantResponse{Kind: AssistantClarification, Text: text}, nil
	case StateEmpty:
		if result.ActiveItemIndex != nil || len(result.Items) != 0 {
			return AssistantResponse{}, fmt.Errorf("malformed empty assistant context")
		}
		if turkish {
			return AssistantResponse{Kind: AssistantGuidance, Text: "Yediğin bir şeyi yazabilir veya bir yiyeceğin besin değerini sorabilirsin."}, nil
		}
		return AssistantResponse{Kind: AssistantGuidance, Text: "You can tell me what you ate or ask about a food's nutrition."}, nil
	default:
		return AssistantResponse{}, fmt.Errorf("unknown assistant state")
	}
}

// ValidateAssistantResponse checks display prose against the authoritative result state.
func ValidateAssistantResponse(result ChatResult) error {
	text := strings.TrimSpace(result.Assistant.Text)
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > maxAssistantResponseRunes {
		return fmt.Errorf("invalid assistant text")
	}
	var expected AssistantResponseKind
	switch result.State {
	case StateReady:
		switch result.Purpose {
		case ChatPurposeNutritionQuery:
			expected = AssistantNutritionAnswer
		case ChatPurposeMealLogging:
			expected = AssistantMealReady
		default:
			return fmt.Errorf("ready assistant has invalid purpose")
		}
	case StateClarificationRequired:
		if result.ActiveItemIndex == nil || *result.ActiveItemIndex < 0 || *result.ActiveItemIndex >= len(result.Items) {
			return fmt.Errorf("clarification assistant has invalid active item")
		}
		expected = AssistantClarification
	case StateEmpty:
		if result.ActiveItemIndex != nil {
			return fmt.Errorf("guidance assistant has invalid state")
		}
		expected = AssistantGuidance
	default:
		return fmt.Errorf("assistant has unknown result state")
	}
	if result.Assistant.Kind != expected {
		return fmt.Errorf("assistant kind does not match result state")
	}
	return nil
}

func nutritionAnswerText(items []Item, turkish bool) string {
	if len(items) != 1 {
		if turkish {
			return fmt.Sprintf("%d yiyecek için besin değerlerini hesapladım.", len(items))
		}
		return fmt.Sprintf("I calculated nutrition values for %d foods.", len(items))
	}
	item := items[0]
	name := userFacingFoodName(item.Food)
	grams := formatDisplayNumber(item.Preview.ResolvedGrams, turkish)
	calories, caloriesKnown := item.Preview.Nutrition.Calories.Value()
	parts := make([]string, 0, 3)
	appendNutrient := func(label string, amount food.NutrientAmount) {
		if value, known := amount.Value(); known {
			parts = append(parts, label+": "+formatDisplayNumber(value, turkish)+" g")
		}
	}
	if turkish {
		appendNutrient("Protein", item.Preview.Nutrition.Protein)
		appendNutrient("karbonhidrat", item.Preview.Nutrition.Carbohydrates)
		appendNutrient("yağ", item.Preview.Nutrition.Fat)
		var sentence string
		if caloriesKnown {
			sentence = fmt.Sprintf("%s g %s: %s kcal.", grams, name, formatDisplayNumber(calories, true))
		} else {
			sentence = fmt.Sprintf("%s g %s için güvenilir kalori değeri mevcut değil.", grams, name)
		}
		if len(parts) > 0 {
			return sentence + " " + strings.Join(parts, ", ") + "."
		}
		return sentence
	}
	appendNutrient("Protein", item.Preview.Nutrition.Protein)
	appendNutrient("carbohydrates", item.Preview.Nutrition.Carbohydrates)
	appendNutrient("fat", item.Preview.Nutrition.Fat)
	var sentence string
	if caloriesKnown {
		sentence = fmt.Sprintf("%s g %s: %s kcal.", grams, name, formatDisplayNumber(calories, false))
	} else {
		sentence = fmt.Sprintf("Reliable calorie data is unavailable for %s g %s.", grams, name)
	}
	if len(parts) > 0 {
		return sentence + " " + strings.Join(parts, ", ") + "."
	}
	return sentence
}

func clarificationText(item Item, turkish bool) (string, error) {
	switch item.Clarification.Kind {
	case ClarificationFoodIdentity:
		return identityClarificationText(item.Clarification.Candidates, turkish), nil
	case ClarificationAmount:
		return amountClarificationText(item, turkish), nil
	default:
		return "", fmt.Errorf("unknown clarification kind")
	}
}

func identityClarificationText(candidates []FoodOption, turkish bool) string {
	if len(candidates) == 0 {
		if turkish {
			return "Bunu güvenle eşleştiremedim. Yiyeceği biraz daha açık tarif eder misin?"
		}
		return "I couldn't match that safely. Could you describe the food more clearly?"
	}
	if len(candidates) == 1 {
		name := optionName(candidates[0])
		if turkish {
			return fmt.Sprintf("Bunu %s olarak buldum. Bunu mu kastettin, yoksa biraz daha açık tarif eder misin?", name)
		}
		return fmt.Sprintf("I found this as %s. Did you mean that, or could you describe it more clearly?", name)
	}
	names := make([]string, 0, min(len(candidates), 3))
	for _, candidate := range candidates[:min(len(candidates), 3)] {
		names = append(names, optionName(candidate))
	}
	if turkish {
		return "Hangisini kastettin: " + strings.Join(names, ", ") + "?"
	}
	return "Which one did you mean: " + strings.Join(names, ", ") + "?"
}

func amountClarificationText(item Item, turkish bool) string {
	name := userFacingFoodName(item.Food)
	portionHint := trustedPortionHint(item.Clarification.Portions)
	switch foodamount.Reason(item.Clarification.Reason) {
	case foodamount.ReasonQuantityRequired:
		if portionHint == "" {
			if turkish {
				return fmt.Sprintf("%s için yaklaşık kaç gram yedin?", name)
			}
			return fmt.Sprintf("About how many grams of %s did you eat?", name)
		}
		if turkish {
			return fmt.Sprintf("Ne kadar %s yedin? Gram olarak veya mevcut porsiyonlardan biriyle belirtebilirsin: %s.", name, portionHint)
		}
		return fmt.Sprintf("How much %s did you eat? You can use grams or one of these available portions: %s.", name, portionHint)
	case foodamount.ReasonUnitRequired:
		quantity := ""
		if item.Intent.Quantity != nil {
			quantity = formatDisplayNumber(*item.Intent.Quantity, turkish) + " "
		}
		if turkish {
			return fmt.Sprintf("%s%s için hangi ölçüyü kullanmak istersin? Gram olarak%s belirtebilirsin.", quantity, name, portionAlternative(portionHint, true))
		}
		return fmt.Sprintf("Which measure should I use for %s%s? You can use grams%s.", quantity, name, portionAlternative(portionHint, false))
	case foodamount.ReasonVolumeRequiresClarification:
		if turkish {
			return "Bunu doğrudan grama çeviremiyorum. Gram olarak" + portionAlternative(portionHint, true) + " belirtir misin?"
		}
		return "I can't safely convert this directly to grams. Please use grams" + portionAlternative(portionHint, false) + "."
	case foodamount.ReasonUnsupportedUnitRequiresClarification:
		if turkish {
			return "Bu ölçüyü güvenle dönüştüremiyorum. Gram olarak" + portionAlternative(portionHint, true) + " belirtebilirsin."
		}
		return "I can't safely convert that measure. You can use grams" + portionAlternative(portionHint, false) + "."
	default:
		if turkish {
			return fmt.Sprintf("%s miktarını gram olarak%s belirtir misin?", name, portionAlternative(portionHint, true))
		}
		return fmt.Sprintf("Please give the amount of %s in grams%s.", name, portionAlternative(portionHint, false))
	}
}

func trustedPortionHint(portions []food.Portion) string {
	measures := make([]string, 0, 3)
	for _, portion := range portions {
		measure := strings.TrimSpace(portion.Measure)
		if measure == "" || containsString(measures, measure) {
			continue
		}
		measures = append(measures, measure)
		if len(measures) == 3 {
			break
		}
	}
	return strings.Join(measures, ", ")
}

func portionAlternative(hint string, turkish bool) string {
	if hint == "" {
		return ""
	}
	if turkish {
		return " veya şu mevcut porsiyonlardan biriyle (" + hint + ")"
	}
	return " or one of these available portions (" + hint + ")"
}

func userFacingFoodName(resolved *ResolvedFood) string {
	if resolved == nil {
		return "food"
	}
	if name := strings.TrimSpace(resolved.DisplayName); name != "" {
		return name
	}
	return strings.TrimSpace(resolved.CanonicalName)
}

func optionName(option FoodOption) string {
	if name := strings.TrimSpace(option.DisplayName); name != "" {
		return name
	}
	return strings.TrimSpace(option.CanonicalName)
}

func formatDisplayNumber(value float64, turkish bool) string {
	formatted := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(value, 'f', 2, 64), "0"), ".")
	if turkish {
		return strings.ReplaceAll(formatted, ".", ",")
	}
	return formatted
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
