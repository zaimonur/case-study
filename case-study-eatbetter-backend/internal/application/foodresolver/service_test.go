package foodresolver

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

type fakeSearcher struct {
	candidates []foodsearch.FoodCandidate
	err        error
	calls      int
	requests   []foodsearch.Request
	contexts   []context.Context
}

func (fake *fakeSearcher) Search(ctx context.Context, request foodsearch.Request) ([]foodsearch.FoodCandidate, error) {
	fake.calls++
	fake.requests = append(fake.requests, request)
	fake.contexts = append(fake.contexts, ctx)
	return fake.candidates, fake.err
}

func TestResolveDecisionPolicy(t *testing.T) {
	t.Parallel()

	exactLocalized := exactCandidate(1, foodsearch.SourceLocalizedDisplay, foodsearch.FormPrimary, false, "localized")
	exactCanonical := exactCandidate(2, foodsearch.SourceCanonicalName, foodsearch.FormPrimary, false, "canonical")
	exactLocalizationAlias := exactCandidate(3, foodsearch.SourceLocalizationAlias, foodsearch.FormPrimary, false, "localization alias")
	exactFoodAlias := exactCandidate(4, foodsearch.SourceFoodAlias, foodsearch.FormPrimary, false, "food alias")
	exactFolded := exactCandidate(5, foodsearch.SourceCanonicalName, foodsearch.FormFolded, false, "folded")
	exactBranded := exactCandidate(6, foodsearch.SourceLocalizedDisplay, foodsearch.FormPrimary, true, "branded")

	tests := []struct {
		name               string
		candidates         []foodsearch.FoodCandidate
		wantState          State
		wantReason         Reason
		wantResolvedFoodID int64
		wantResolvedName   string
	}{
		{name: "zero candidates", candidates: nil, wantState: StateNotFound, wantReason: ReasonNoCandidates},
		{name: "exact localized display", candidates: []foodsearch.FoodCandidate{exactLocalized}, wantState: StateResolved, wantReason: ReasonUniqueExactIdentity, wantResolvedFoodID: 1},
		{name: "exact canonical name", candidates: []foodsearch.FoodCandidate{exactCanonical}, wantState: StateResolved, wantReason: ReasonUniqueExactIdentity, wantResolvedFoodID: 2},
		{name: "exact localization alias", candidates: []foodsearch.FoodCandidate{exactLocalizationAlias}, wantState: StateResolved, wantReason: ReasonUniqueExactIdentity, wantResolvedFoodID: 3},
		{name: "exact food alias", candidates: []foodsearch.FoodCandidate{exactFoodAlias}, wantState: StateResolved, wantReason: ReasonUniqueExactIdentity, wantResolvedFoodID: 4},
		{name: "exact folded form", candidates: []foodsearch.FoodCandidate{exactFolded}, wantState: StateResolved, wantReason: ReasonUniqueExactIdentity, wantResolvedFoodID: 5},
		{name: "branded product with exact identity source", candidates: []foodsearch.FoodCandidate{exactBranded}, wantState: StateResolved, wantReason: ReasonUniqueExactIdentity, wantResolvedFoodID: 6},
		{
			name: "one exact identity with weaker candidates",
			candidates: []foodsearch.FoodCandidate{
				weakCandidate(10, foodsearch.MatchWord, 0.8), exactLocalized,
				weakCandidate(11, foodsearch.MatchPrefix, 0.9), weakCandidate(12, foodsearch.MatchFuzzy, 0.99),
			},
			wantState: StateResolved, wantReason: ReasonUniqueExactIdentity, wantResolvedFoodID: 1,
		},
		{
			name:       "two distinct exact identities",
			candidates: []foodsearch.FoodCandidate{exactLocalized, exactCanonical},
			wantState:  StateAmbiguous, wantReason: ReasonMultipleExactIdentities,
		},
		{
			name:       "distinct exact identities from different eligible sources",
			candidates: []foodsearch.FoodCandidate{exactLocalizationAlias, exactFoodAlias},
			wantState:  StateAmbiguous, wantReason: ReasonMultipleExactIdentities,
		},
		{
			name: "broad Turkish Chicken aliases remain ambiguous",
			candidates: []foodsearch.FoodCandidate{
				exactCandidate(7, foodsearch.SourceFoodAlias, foodsearch.FormPrimary, false, "Chicken raw"),
				exactCandidate(8, foodsearch.SourceFoodAlias, foodsearch.FormPrimary, false, "Chicken roasted"),
			},
			wantState: StateAmbiguous, wantReason: ReasonMultipleExactIdentities,
		},
		{
			name: "duplicate exact FoodID uses first eligible occurrence",
			candidates: []foodsearch.FoodCandidate{
				exactCandidate(20, foodsearch.SourceCanonicalName, foodsearch.FormPrimary, false, "first"),
				weakCandidate(21, foodsearch.MatchWord, 0.8),
				exactCandidate(20, foodsearch.SourceLocalizedDisplay, foodsearch.FormFolded, true, "second"),
			},
			wantState: StateResolved, wantReason: ReasonUniqueExactIdentity,
			wantResolvedFoodID: 20, wantResolvedName: "first",
		},
		{name: "single word candidate", candidates: []foodsearch.FoodCandidate{weakCandidate(30, foodsearch.MatchWord, 0.9)}, wantState: StateAmbiguous, wantReason: ReasonNoSafeExactIdentity},
		{name: "single prefix candidate", candidates: []foodsearch.FoodCandidate{weakCandidate(31, foodsearch.MatchPrefix, 0.9)}, wantState: StateAmbiguous, wantReason: ReasonNoSafeExactIdentity},
		{name: "single fuzzy candidate", candidates: []foodsearch.FoodCandidate{weakCandidate(32, foodsearch.MatchFuzzy, 0.5)}, wantState: StateAmbiguous, wantReason: ReasonNoSafeExactIdentity},
		{name: "high-similarity fuzzy candidate", candidates: []foodsearch.FoodCandidate{weakCandidate(33, foodsearch.MatchFuzzy, 0.99)}, wantState: StateAmbiguous, wantReason: ReasonNoSafeExactIdentity},
		{
			name:       "single exact brand source",
			candidates: []foodsearch.FoodCandidate{exactCandidate(40, foodsearch.SourceBrand, foodsearch.FormPrimary, true, "brand")},
			wantState:  StateAmbiguous, wantReason: ReasonNoSafeExactIdentity,
		},
		{
			name: "multiple exact brand sources",
			candidates: []foodsearch.FoodCandidate{
				exactCandidate(40, foodsearch.SourceBrand, foodsearch.FormPrimary, true, "brand one"),
				exactCandidate(41, foodsearch.SourceBrand, foodsearch.FormFolded, true, "brand two"),
			},
			wantState: StateAmbiguous, wantReason: ReasonNoSafeExactIdentity,
		},
		{
			name:       "unique exact identity in full candidate set",
			candidates: append([]foodsearch.FoodCandidate{exactLocalized}, weakCandidates(foodsearch.DefaultLimit-1)...),
			wantState:  StateAmbiguous, wantReason: ReasonCandidateSetMayBeTruncated,
		},
		{
			name:       "multiple exact identities take precedence at limit",
			candidates: append([]foodsearch.FoodCandidate{exactLocalized, exactCanonical}, weakCandidates(foodsearch.DefaultLimit-2)...),
			wantState:  StateAmbiguous, wantReason: ReasonMultipleExactIdentities,
		},
		{
			name:       "weak-only full candidate set",
			candidates: weakCandidates(foodsearch.DefaultLimit),
			wantState:  StateAmbiguous, wantReason: ReasonNoSafeExactIdentity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			searcher := &fakeSearcher{candidates: tt.candidates}
			resolution, err := NewService(searcher).Resolve(context.Background(), Request{
				Intent: foodintent.FoodIntent{Query: "elma"}, Locale: "tr-TR",
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if searcher.calls != 1 {
				t.Fatalf("search calls = %d, want 1", searcher.calls)
			}
			if resolution.State != tt.wantState || resolution.Reason != tt.wantReason {
				t.Fatalf("state/reason = %s/%s, want %s/%s", resolution.State, resolution.Reason, tt.wantState, tt.wantReason)
			}
			if !reflect.DeepEqual(resolution.Candidates, nonNilCandidates(tt.candidates)) {
				t.Fatalf("candidate order changed:\n got: %#v\nwant: %#v", resolution.Candidates, nonNilCandidates(tt.candidates))
			}
			assertResolutionInvariants(t, resolution)
			if tt.wantResolvedFoodID != 0 {
				if resolution.Resolved.FoodID != tt.wantResolvedFoodID {
					t.Fatalf("resolved FoodID = %d, want %d", resolution.Resolved.FoodID, tt.wantResolvedFoodID)
				}
				if tt.wantResolvedName != "" && resolution.Resolved.DisplayName != tt.wantResolvedName {
					t.Fatalf("resolved representative = %q, want %q", resolution.Resolved.DisplayName, tt.wantResolvedName)
				}
			}
		})
	}
}

func TestResolveForwardsOnlyIdentityInputsUnchanged(t *testing.T) {
	t.Parallel()

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "same-context")
	quantity := 2.0
	unitHint := "adet"
	intent := foodintent.FoodIntent{Query: "  Haşlanmış Yumurta  ", Quantity: &quantity, UnitHint: &unitHint}
	searcher := &fakeSearcher{candidates: []foodsearch.FoodCandidate{
		exactCandidate(51, foodsearch.SourceCanonicalName, foodsearch.FormPrimary, false, "egg"),
	}}

	resolution, err := NewService(searcher).Resolve(ctx, Request{Intent: intent, Locale: "tr-Latn-TR"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if searcher.calls != 1 || len(searcher.requests) != 1 {
		t.Fatalf("search calls/requests = %d/%d, want 1/1", searcher.calls, len(searcher.requests))
	}
	wantSearchRequest := foodsearch.Request{
		Query: "  Haşlanmış Yumurta  ", Locale: "tr-Latn-TR",
		Limit: foodsearch.DefaultLimit, LimitSet: true,
	}
	if searcher.requests[0] != wantSearchRequest {
		t.Fatalf("search request = %#v, want %#v", searcher.requests[0], wantSearchRequest)
	}
	if searcher.contexts[0] != ctx {
		t.Fatal("resolver did not pass the caller context unchanged")
	}
	if intent.Query != "  Haşlanmış Yumurta  " || *intent.Quantity != 2 || *intent.UnitHint != "adet" {
		t.Fatalf("intent was modified: %#v", intent)
	}
	assertResolutionInvariants(t, resolution)

	otherQuantity := 900.0
	otherUnit := "g"
	otherSearcher := &fakeSearcher{candidates: searcher.candidates}
	other, err := NewService(otherSearcher).Resolve(ctx, Request{
		Intent: foodintent.FoodIntent{Query: intent.Query, Quantity: &otherQuantity, UnitHint: &otherUnit},
		Locale: "tr-Latn-TR",
	})
	if err != nil {
		t.Fatalf("Resolve with other amount: %v", err)
	}
	if resolution.State != other.State || resolution.Reason != other.Reason || resolution.Resolved.FoodID != other.Resolved.FoodID {
		t.Fatalf("amount changed identity resolution: first=%#v other=%#v", resolution, other)
	}
}

func TestResolveClassifiesSearchErrorsWithoutResolution(t *testing.T) {
	t.Parallel()

	validationCause := &foodsearch.ValidationError{Field: "q"}
	operationalCause := errors.New("database implementation detail")
	tests := []struct {
		name      string
		err       error
		wantKind  ErrorKind
		wantCause error
	}{
		{name: "typed validation", err: fmt.Errorf("wrapped: %w", validationCause), wantKind: ErrorInvalidInput, wantCause: validationCause},
		{name: "search failure", err: operationalCause, wantKind: ErrorSearchFailure, wantCause: operationalCause},
		{name: "wrapped cancellation", err: fmt.Errorf("search interrupted: %w", context.Canceled), wantKind: ErrorCanceled, wantCause: context.Canceled},
		{name: "wrapped deadline", err: fmt.Errorf("search interrupted: %w", context.DeadlineExceeded), wantKind: ErrorTimeout, wantCause: context.DeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			searcher := &fakeSearcher{err: tt.err}
			resolution, err := NewService(searcher).Resolve(context.Background(), Request{
				Intent: foodintent.FoodIntent{Query: "elma"}, Locale: "tr-TR",
			})
			if !IsKind(err, tt.wantKind) {
				t.Fatalf("error = %v, want kind %s", err, tt.wantKind)
			}
			if !errors.Is(err, tt.wantCause) {
				t.Fatalf("error = %v, want wrapped cause %v", err, tt.wantCause)
			}
			if tt.wantKind == ErrorSearchFailure && strings.Contains(err.Error(), operationalCause.Error()) {
				t.Fatalf("stable error exposed implementation detail: %v", err)
			}
			if resolution.State != "" || resolution.Reason != "" || resolution.Resolved != nil || resolution.Candidates != nil {
				t.Fatalf("resolution = %#v, want zero result on error", resolution)
			}
			if searcher.calls != 1 {
				t.Fatalf("search calls = %d, want 1", searcher.calls)
			}
		})
	}
}

func assertResolutionInvariants(t *testing.T, resolution Resolution) {
	t.Helper()
	switch resolution.State {
	case StateResolved:
		if resolution.Resolved == nil || len(resolution.Candidates) == 0 {
			t.Fatalf("invalid resolved result: %#v", resolution)
		}
	case StateAmbiguous:
		if resolution.Resolved != nil || len(resolution.Candidates) == 0 {
			t.Fatalf("invalid ambiguous result: %#v", resolution)
		}
	case StateNotFound:
		if resolution.Resolved != nil || resolution.Candidates == nil || len(resolution.Candidates) != 0 {
			t.Fatalf("invalid not-found result: %#v", resolution)
		}
	default:
		t.Fatalf("unknown resolution state: %q", resolution.State)
	}
}

func exactCandidate(id int64, source foodsearch.MatchSource, form foodsearch.MatchForm, branded bool, name string) foodsearch.FoodCandidate {
	return foodsearch.FoodCandidate{
		FoodID: id, CanonicalName: name, DisplayName: name, IsBranded: branded,
		Match: foodsearch.MatchMetadata{Class: foodsearch.MatchExact, Source: source, Form: form, Similarity: 1},
	}
}

func weakCandidate(id int64, class foodsearch.MatchClass, similarity float64) foodsearch.FoodCandidate {
	return foodsearch.FoodCandidate{
		FoodID: id, CanonicalName: fmt.Sprintf("food-%d", id), DisplayName: fmt.Sprintf("food-%d", id),
		Match: foodsearch.MatchMetadata{Class: class, Source: foodsearch.SourceCanonicalName, Form: foodsearch.FormPrimary, Similarity: similarity},
	}
}

func weakCandidates(count int) []foodsearch.FoodCandidate {
	candidates := make([]foodsearch.FoodCandidate, 0, count)
	for index := range count {
		candidates = append(candidates, weakCandidate(int64(100+index), foodsearch.MatchFuzzy, 0.99))
	}
	return candidates
}

func nonNilCandidates(candidates []foodsearch.FoodCandidate) []foodsearch.FoodCandidate {
	if candidates == nil {
		return []foodsearch.FoodCandidate{}
	}
	return candidates
}
