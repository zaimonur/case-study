package foodextraction

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
)

type stubExtractor struct {
	result TextFoodExtraction
	err    error
	calls  int
}

func (stub *stubExtractor) Extract(context.Context, string) (TextFoodExtraction, error) {
	stub.calls++
	return stub.result, stub.err
}

func TestServiceRejectsInvalidInputBeforeProvider(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"", " \n\t ", strings.Repeat("ş", MaxInputRunes+1)} {
		provider := &stubExtractor{}
		_, err := NewService(provider).Extract(context.Background(), text)
		if !IsKind(err, ErrorInvalidInput) {
			t.Fatalf("Extract(%q) error = %v, want invalid input", text[:min(len(text), 20)], err)
		}
		if provider.calls != 0 {
			t.Fatalf("provider calls = %d, want 0", provider.calls)
		}
	}
}

func TestServiceAllowsEmptyExtraction(t *testing.T) {
	t.Parallel()

	result, err := NewService(&stubExtractor{result: TextFoodExtraction{}}).Extract(context.Background(), "Bugün hiçbir şey yemedim.")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("Items = %#v, want non-nil empty slice", result.Items)
	}
}

func TestServiceValidatesRepeatedMentionsAgainstSeparateOrderedOccurrences(t *testing.T) {
	t.Parallel()

	quantityTwo, quantityOne := 2.0, 1.0
	unit := " tane "
	provider := &stubExtractor{result: TextFoodExtraction{Items: []ExtractedTextFoodIntent{
		{Mention: "yumurta", Intent: foodintent.FoodIntent{Query: " yumurta ", Quantity: &quantityTwo, UnitHint: &unit}},
		{Mention: "yumurta", Intent: foodintent.FoodIntent{Query: "yumurta", Quantity: &quantityOne, UnitHint: &unit}},
	}}}
	result, err := NewService(provider).Extract(context.Background(), "2 yumurta ve 1 yumurta daha")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("item count = %d, want 2", len(result.Items))
	}
	if result.Items[0].Intent.Query != "yumurta" || *result.Items[0].Intent.UnitHint != "adet" {
		t.Fatalf("normalized first item = %#v", result.Items[0])
	}
}

func TestServiceRejectsReusedOrOutOfOrderMentionOccurrences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		items  []ExtractedTextFoodIntent
	}{
		{
			name: "one occurrence reused", source: "yumurta",
			items: []ExtractedTextFoodIntent{validItem("yumurta"), validItem("yumurta")},
		},
		{
			name: "source order reversed", source: "elma muz",
			items: []ExtractedTextFoodIntent{validItem("muz"), validItem("elma")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewService(&stubExtractor{result: TextFoodExtraction{Items: tt.items}}).Extract(context.Background(), tt.source)
			if !IsKind(err, ErrorInvalidProviderOutput) {
				t.Fatalf("error = %v, want invalid provider output", err)
			}
		})
	}
}

func TestServiceRejectsInvalidProviderSemantics(t *testing.T) {
	t.Parallel()

	zero, negative, nan, infinity := 0.0, -1.0, math.NaN(), math.Inf(1)
	emptyUnit, longUnit := "  ", strings.Repeat("ü", MaxUnitHintRunes+1)
	tooMany := make([]ExtractedTextFoodIntent, MaxItems+1)
	for index := range tooMany {
		tooMany[index] = validItem("elma")
	}
	tests := []struct {
		name   string
		source string
		items  []ExtractedTextFoodIntent
	}{
		{name: "too many items", source: strings.Repeat("elma ", MaxItems+1), items: tooMany},
		{name: "empty mention", source: "elma", items: []ExtractedTextFoodIntent{{Intent: foodintent.FoodIntent{Query: "elma"}}}},
		{name: "whitespace mention", source: " elma", items: []ExtractedTextFoodIntent{{Mention: " ", Intent: foodintent.FoodIntent{Query: "elma"}}}},
		{name: "mention not verbatim", source: "Elma yedim", items: []ExtractedTextFoodIntent{validItem("elma")}},
		{name: "short query", source: "x", items: []ExtractedTextFoodIntent{{Mention: "x", Intent: foodintent.FoodIntent{Query: "x"}}}},
		{name: "long query", source: "elma", items: []ExtractedTextFoodIntent{{Mention: "elma", Intent: foodintent.FoodIntent{Query: strings.Repeat("a", 121)}}}},
		{name: "zero quantity", source: "elma", items: []ExtractedTextFoodIntent{itemWithQuantity("elma", &zero)}},
		{name: "negative quantity", source: "elma", items: []ExtractedTextFoodIntent{itemWithQuantity("elma", &negative)}},
		{name: "NaN quantity", source: "elma", items: []ExtractedTextFoodIntent{itemWithQuantity("elma", &nan)}},
		{name: "infinite quantity", source: "elma", items: []ExtractedTextFoodIntent{itemWithQuantity("elma", &infinity)}},
		{name: "empty unit", source: "elma", items: []ExtractedTextFoodIntent{itemWithUnit("elma", &emptyUnit)}},
		{name: "long unit", source: "elma", items: []ExtractedTextFoodIntent{itemWithUnit("elma", &longUnit)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewService(&stubExtractor{result: TextFoodExtraction{Items: tt.items}}).Extract(context.Background(), tt.source)
			if !IsKind(err, ErrorInvalidProviderOutput) {
				t.Fatalf("error = %v, want invalid provider output", err)
			}
		})
	}
}

func TestServicePreservesProviderErrorCategory(t *testing.T) {
	t.Parallel()

	providerError := NewError(ErrorRateLimit, errors.New("limited"))
	_, err := NewService(&stubExtractor{err: providerError}).Extract(context.Background(), "elma")
	if !IsKind(err, ErrorRateLimit) {
		t.Fatalf("error = %v, want rate limit", err)
	}
}

func validItem(mention string) ExtractedTextFoodIntent {
	return ExtractedTextFoodIntent{Mention: mention, Intent: foodintent.FoodIntent{Query: mention}}
}

func itemWithQuantity(mention string, quantity *float64) ExtractedTextFoodIntent {
	item := validItem(mention)
	item.Intent.Quantity = quantity
	return item
}

func itemWithUnit(mention string, unit *string) ExtractedTextFoodIntent {
	item := validItem(mention)
	item.Intent.UnitHint = unit
	return item
}
