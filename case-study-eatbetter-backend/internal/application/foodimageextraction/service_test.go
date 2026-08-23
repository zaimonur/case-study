package foodimageextraction

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
)

type stubExtractor struct {
	result ImageFoodExtraction
	err    error
	calls  int
	input  ImageInput
}

func (stub *stubExtractor) Extract(_ context.Context, input ImageInput) (ImageFoodExtraction, error) {
	stub.calls++
	stub.input = input
	return stub.result, stub.err
}

func TestServiceRejectsInvalidInputBeforeProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input ImageInput
	}{
		{name: "empty data", input: ImageInput{MIMEType: "image/jpeg"}},
		{name: "unsupported MIME", input: ImageInput{Data: []byte{1}, MIMEType: "image/gif"}},
		{name: "oversized", input: ImageInput{Data: make([]byte, MaxImageBytes+1), MIMEType: "image/png"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			provider := &stubExtractor{}
			_, err := NewService(provider).Extract(context.Background(), tt.input)
			if !IsKind(err, ErrorInvalidInput) {
				t.Fatalf("error = %v, want invalid_input", err)
			}
			if provider.calls != 0 {
				t.Fatalf("provider calls = %d, want 0", provider.calls)
			}
		})
	}
}

func TestServiceAcceptsSupportedMIMETypes(t *testing.T) {
	t.Parallel()

	for _, mimeType := range []string{"image/jpeg", "image/png", "image/webp"} {
		t.Run(mimeType, func(t *testing.T) {
			t.Parallel()
			provider := &stubExtractor{result: ImageFoodExtraction{Items: []ExtractedImageFoodIntent{}}}
			_, err := NewService(provider).Extract(context.Background(), ImageInput{Data: []byte{1}, MIMEType: mimeType})
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if provider.calls != 1 || provider.input.MIMEType != mimeType {
				t.Fatalf("provider calls/input = %d/%q", provider.calls, provider.input.MIMEType)
			}
		})
	}
}

func TestServiceNormalizesMIMETypeConservatively(t *testing.T) {
	t.Parallel()

	provider := &stubExtractor{result: ImageFoodExtraction{Items: []ExtractedImageFoodIntent{}}}
	_, err := NewService(provider).Extract(context.Background(), ImageInput{Data: []byte{1}, MIMEType: " Image/JPEG "})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if provider.input.MIMEType != "image/jpeg" {
		t.Fatalf("MIMEType = %q, want image/jpeg", provider.input.MIMEType)
	}
}

func TestServiceRequiresConfiguredExtractor(t *testing.T) {
	t.Parallel()

	input := ImageInput{Data: []byte{1}, MIMEType: "image/jpeg"}
	var nilService *Service
	for _, service := range []*Service{nilService, NewService(nil)} {
		_, err := service.Extract(context.Background(), input)
		if !IsKind(err, ErrorProviderConfiguration) {
			t.Fatalf("error = %v, want provider_configuration", err)
		}
	}
}

func TestServiceMapsExtractorErrors(t *testing.T) {
	t.Parallel()

	typed := NewError(ErrorRateLimit, errors.New("limited"))
	tests := []struct {
		name string
		err  error
		kind ErrorKind
	}{
		{name: "typed preserved", err: typed, kind: ErrorRateLimit},
		{name: "canceled", err: context.Canceled, kind: ErrorCanceled},
		{name: "deadline", err: context.DeadlineExceeded, kind: ErrorTimeout},
		{name: "unknown", err: errors.New("provider detail"), kind: ErrorProviderFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewService(&stubExtractor{err: tt.err}).Extract(context.Background(), ImageInput{Data: []byte{1}, MIMEType: "image/png"})
			if !IsKind(err, tt.kind) {
				t.Fatalf("error = %v, want %s", err, tt.kind)
			}
			if tt.err == typed && err != typed {
				t.Fatalf("typed error was not preserved: got %v", err)
			}
			if strings.Contains(err.Error(), "provider detail") {
				t.Fatalf("error exposed provider detail: %v", err)
			}
		})
	}
}

func TestServiceNormalizesEmptyItems(t *testing.T) {
	t.Parallel()

	for _, items := range [][]ExtractedImageFoodIntent{nil, {}} {
		result, err := NewService(&stubExtractor{result: ImageFoodExtraction{Items: items}}).Extract(
			context.Background(), ImageInput{Data: []byte{1}, MIMEType: "image/webp"},
		)
		if err != nil {
			t.Fatalf("Extract: %v", err)
		}
		if result.Items == nil || len(result.Items) != 0 {
			t.Fatalf("Items = %#v, want non-nil empty slice", result.Items)
		}
	}
}

func TestServiceRejectsInvalidProviderOutput(t *testing.T) {
	t.Parallel()

	quantity := 1.0
	unit := "piece"
	tooMany := make([]ExtractedImageFoodIntent, MaxItems+1)
	for index := range tooMany {
		tooMany[index] = validItem()
	}
	tests := []struct {
		name  string
		items []ExtractedImageFoodIntent
	}{
		{name: "too many", items: tooMany},
		{name: "blank observation", items: []ExtractedImageFoodIntent{{Intent: foodintent.FoodIntent{Query: "apple"}}}},
		{name: "long observation", items: []ExtractedImageFoodIntent{{Observation: strings.Repeat("ş", MaxObservationRunes+1), Intent: foodintent.FoodIntent{Query: "apple"}}}},
		{name: "blank query", items: []ExtractedImageFoodIntent{{Observation: "an apple"}}},
		{name: "short query", items: []ExtractedImageFoodIntent{{Observation: "an apple", Intent: foodintent.FoodIntent{Query: "x"}}}},
		{name: "long query", items: []ExtractedImageFoodIntent{{Observation: "an apple", Intent: foodintent.FoodIntent{Query: strings.Repeat("x", MaxQueryRunes+1)}}}},
		{name: "quantity", items: []ExtractedImageFoodIntent{{Observation: "an apple", Intent: foodintent.FoodIntent{Query: "apple", Quantity: &quantity}}}},
		{name: "unit", items: []ExtractedImageFoodIntent{{Observation: "an apple", Intent: foodintent.FoodIntent{Query: "apple", UnitHint: &unit}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewService(&stubExtractor{result: ImageFoodExtraction{Items: tt.items}}).Extract(
				context.Background(), ImageInput{Data: []byte{1}, MIMEType: "image/jpeg"},
			)
			if !IsKind(err, ErrorInvalidProviderOutput) {
				t.Fatalf("error = %v, want invalid_provider_output", err)
			}
		})
	}
}

func TestServiceTrimsValidOutputWithoutAddingAmountEvidence(t *testing.T) {
	t.Parallel()

	result, err := NewService(&stubExtractor{result: ImageFoodExtraction{Items: []ExtractedImageFoodIntent{
		{Observation: "  visibly sliced apple  ", Intent: foodintent.FoodIntent{Query: "  sliced apple  "}},
	}}}).Extract(context.Background(), ImageInput{Data: []byte{1}, MIMEType: "image/jpeg"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	item := result.Items[0]
	if item.Observation != "visibly sliced apple" || item.Intent.Query != "sliced apple" {
		t.Fatalf("item = %#v, want trimmed values", item)
	}
	if item.Intent.Quantity != nil || item.Intent.UnitHint != nil {
		t.Fatalf("amount evidence must remain nil: %#v", item.Intent)
	}
}

func validItem() ExtractedImageFoodIntent {
	return ExtractedImageFoodIntent{Observation: "an apple", Intent: foodintent.FoodIntent{Query: "apple"}}
}
