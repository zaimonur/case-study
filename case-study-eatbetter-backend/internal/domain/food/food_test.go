package food

import (
	"math"
	"testing"
)

func TestNewFood(t *testing.T) {
	t.Parallel()

	brand := "  Example Brand  "
	got, err := NewFood("  Banana, raw  ", &brand)
	if err != nil {
		t.Fatalf("NewFood() error = %v", err)
	}
	if got.CanonicalName != "Banana, raw" {
		t.Fatalf("CanonicalName = %q, want %q", got.CanonicalName, "Banana, raw")
	}
	if got.Brand == nil || *got.Brand != "Example Brand" {
		t.Fatalf("Brand = %v, want Example Brand", got.Brand)
	}
}

func TestNewFoodAcceptsMissingBrand(t *testing.T) {
	t.Parallel()

	got, err := NewFood("Banana, raw", nil)
	if err != nil {
		t.Fatalf("NewFood() error = %v", err)
	}
	if got.Brand != nil {
		t.Fatalf("Brand = %v, want nil", got.Brand)
	}
}

func TestNewFoodRejectsInvalidNames(t *testing.T) {
	t.Parallel()

	if _, err := NewFood("  ", nil); err == nil {
		t.Fatal("NewFood() error = nil, want canonical name validation error")
	}

	blankBrand := "\t"
	if _, err := NewFood("Banana", &blankBrand); err == nil {
		t.Fatal("NewFood() error = nil, want brand validation error")
	}
}

func TestNutrientAmountDistinguishesUnknownFromKnownZero(t *testing.T) {
	t.Parallel()

	var unknown NutrientAmount
	if unknown.IsKnown() {
		t.Fatal("zero-value NutrientAmount is known, want unknown")
	}
	if value, known := unknown.Value(); known || value != 0 {
		t.Fatalf("unknown.Value() = (%v, %v), want (0, false)", value, known)
	}

	zero, err := NewNutrientAmount(0)
	if err != nil {
		t.Fatalf("NewNutrientAmount(0) error = %v", err)
	}
	if value, known := zero.Value(); !known || value != 0 {
		t.Fatalf("known zero.Value() = (%v, %v), want (0, true)", value, known)
	}
}

func TestNewNutrientAmountRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{-0.1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		value := value
		t.Run("invalid", func(t *testing.T) {
			t.Parallel()
			if _, err := NewNutrientAmount(value); err == nil {
				t.Fatalf("NewNutrientAmount(%v) error = nil", value)
			}
		})
	}
}

func TestNewNutritionRequiresKnownNutrient(t *testing.T) {
	t.Parallel()

	if _, err := NewNutrition(1, NutrientAmount{}, NutrientAmount{}, NutrientAmount{}, NutrientAmount{}); err == nil {
		t.Fatal("NewNutrition() error = nil, want all-unknown validation error")
	}

	knownZero, err := NewNutrientAmount(0)
	if err != nil {
		t.Fatalf("NewNutrientAmount(0) error = %v", err)
	}
	nutrition, err := NewNutrition(42, NutrientAmount{}, knownZero, NutrientAmount{}, NutrientAmount{})
	if err != nil {
		t.Fatalf("NewNutrition() error = %v", err)
	}
	if nutrition.FoodID != 42 || !nutrition.ProteinPer100g.IsKnown() || nutrition.CaloriesPer100g.IsKnown() {
		t.Fatalf("unexpected nutrition: %+v", nutrition)
	}
}

func TestNewPortion(t *testing.T) {
	t.Parallel()

	portion, err := NewPortion(7, 1, "  large egg  ", 50)
	if err != nil {
		t.Fatalf("NewPortion() error = %v", err)
	}
	if portion.FoodID != 7 || portion.Measure != "large egg" {
		t.Fatalf("unexpected portion: %+v", portion)
	}
}

func TestNewPortionRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		amount  float64
		measure string
		grams   float64
	}{
		{name: "zero amount", amount: 0, measure: "slice", grams: 30},
		{name: "negative amount", amount: -1, measure: "slice", grams: 30},
		{name: "NaN amount", amount: math.NaN(), measure: "slice", grams: 30},
		{name: "infinite amount", amount: math.Inf(1), measure: "slice", grams: 30},
		{name: "blank measure", amount: 1, measure: "  ", grams: 30},
		{name: "zero grams", amount: 1, measure: "slice", grams: 0},
		{name: "negative grams", amount: 1, measure: "slice", grams: -30},
		{name: "NaN grams", amount: 1, measure: "slice", grams: math.NaN()},
		{name: "infinite grams", amount: 1, measure: "slice", grams: math.Inf(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewPortion(1, tt.amount, tt.measure, tt.grams); err == nil {
				t.Fatal("NewPortion() error = nil, want validation error")
			}
		})
	}
}

func TestNewFoodAlias(t *testing.T) {
	t.Parallel()

	languageTag := "  tr  "
	alias, err := NewFoodAlias(8, "  haşlanmış yumurta  ", &languageTag)
	if err != nil {
		t.Fatalf("NewFoodAlias() error = %v", err)
	}
	if alias.Alias != "haşlanmış yumurta" || alias.LanguageTag == nil || *alias.LanguageTag != "tr" {
		t.Fatalf("unexpected alias: %+v", alias)
	}

	withoutLanguage, err := NewFoodAlias(8, "boiled egg", nil)
	if err != nil {
		t.Fatalf("NewFoodAlias() without language error = %v", err)
	}
	if withoutLanguage.LanguageTag != nil {
		t.Fatalf("LanguageTag = %v, want nil", withoutLanguage.LanguageTag)
	}
}

func TestNewFoodAliasRejectsBlankValues(t *testing.T) {
	t.Parallel()

	if _, err := NewFoodAlias(1, "  ", nil); err == nil {
		t.Fatal("NewFoodAlias() error = nil, want alias validation error")
	}

	blankLanguageTag := " "
	if _, err := NewFoodAlias(1, "egg", &blankLanguageTag); err == nil {
		t.Fatal("NewFoodAlias() error = nil, want language tag validation error")
	}
}

func TestNewExternalFoodReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source FoodSource
	}{
		{name: "USDA", source: FoodSourceUSDA},
		{name: "Open Food Facts", source: FoodSourceOpenFoodFacts},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ref, err := NewExternalFoodReference(9, tt.source, "  123456  ")
			if err != nil {
				t.Fatalf("NewExternalFoodReference() error = %v", err)
			}
			if ref.FoodID != 9 || ref.Source != tt.source || ref.ExternalID != "123456" {
				t.Fatalf("unexpected reference: %+v", ref)
			}
		})
	}
}

func TestNewExternalFoodReferenceRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	if _, err := NewExternalFoodReference(1, FoodSource("other"), "123"); err == nil {
		t.Fatal("NewExternalFoodReference() error = nil, want source validation error")
	}
	if _, err := NewExternalFoodReference(1, FoodSourceUSDA, "  "); err == nil {
		t.Fatal("NewExternalFoodReference() error = nil, want external ID validation error")
	}
}

func TestNewFoodIdentifierPreservesGTINLeadingZeroes(t *testing.T) {
	t.Parallel()

	identifier, err := NewFoodIdentifier(12, IdentifierSchemeGTINUPC, "  00027000612323  ")
	if err != nil {
		t.Fatalf("NewFoodIdentifier() error = %v", err)
	}
	if identifier.FoodID != 12 || identifier.Scheme != IdentifierSchemeGTINUPC || identifier.Value != "00027000612323" {
		t.Fatalf("unexpected identifier: %+v", identifier)
	}
}

func TestNewFoodIdentifierRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	if _, err := NewFoodIdentifier(1, IdentifierScheme("other"), "123"); err == nil {
		t.Fatal("NewFoodIdentifier() error = nil, want scheme validation error")
	}
	if _, err := NewFoodIdentifier(1, IdentifierSchemeGTINUPC, "  "); err == nil {
		t.Fatal("NewFoodIdentifier() error = nil, want value validation error")
	}
}
