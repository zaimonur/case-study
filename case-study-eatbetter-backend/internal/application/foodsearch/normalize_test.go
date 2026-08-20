package foodsearch

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestNormalizeTurkishPrecisionAndASCIITolerance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, primary, folded string
	}{
		{" İ ", "i", "i"},
		{"i", "i", "i"},
		{"I", "ı", "i"},
		{"ı", "ı", "i"},
		{" SÜT ", "süt", "sut"},
		{"sut", "sut", "sut"},
		{"ÇİĞ", "çiğ", "cig"},
		{"cig", "cig", "cig"},
		{"  Tam   Yağlı\tSüt ", "tam yağlı süt", "tam yagli sut"},
		{"Süt, tam-yağlı!", "süt tam yağlı", "sut tam yagli"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got := Normalize(test.input)
			if got.Primary != test.primary || got.Folded != test.folded {
				t.Fatalf("Normalize(%q) = %+v, want primary=%q folded=%q", test.input, got, test.primary, test.folded)
			}
		})
	}
}

func TestNormalizeNFCAndNFDAreEquivalent(t *testing.T) {
	t.Parallel()
	nfc := "ÇİĞ süt"
	nfd := norm.NFD.String(nfc)
	if got, want := Normalize(nfd), Normalize(nfc); got != want {
		t.Fatalf("NFD = %+v, NFC = %+v", got, want)
	}
}

type recordingRepository struct {
	query             Query
	phrases           []BrandPhrase
	brandMatch        *BrandMatch
	brandedQuery      BrandedQuery
	candidates        []FoodCandidate
	brandedCandidates []FoodCandidate
	err               error
}

func (r *recordingRepository) Search(_ context.Context, query Query) ([]FoodCandidate, error) {
	r.query = query
	return r.candidates, r.err
}

func (r *recordingRepository) ResolveBrand(_ context.Context, phrases []BrandPhrase) (*BrandMatch, error) {
	r.phrases = phrases
	return r.brandMatch, r.err
}

func (r *recordingRepository) SearchBranded(_ context.Context, query BrandedQuery) ([]FoodCandidate, error) {
	r.brandedQuery = query
	return r.brandedCandidates, r.err
}

func TestExplicitBrandIntentRemovesPersistedPhraseAtEitherPosition(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		query       string
		match       BrandMatch
		wantProduct string
	}{
		{"Kroger milk", BrandMatch{Primary: "kroger", Folded: "kroger", Start: 0, End: 1}, "milk"},
		{"milk Kroger", BrandMatch{Primary: "kroger", Folded: "kroger", Start: 1, End: 2}, "milk"},
		{"organic Great Value milk", BrandMatch{Primary: "great value", Folded: "great value", Start: 1, End: 3}, "organic milk"},
	} {
		repository := &recordingRepository{brandMatch: &test.match, brandedCandidates: []FoodCandidate{{FoodID: 9}}}
		got, err := NewService(repository).Search(context.Background(), Request{Query: test.query})
		if err != nil || len(got) != 1 || got[0].FoodID != 9 {
			t.Fatalf("Search(%q) = %+v, %v", test.query, got, err)
		}
		if repository.brandedQuery.Primary != test.wantProduct || repository.brandedQuery.BrandPrimary != test.match.Primary {
			t.Fatalf("Search(%q) branded query = %+v", test.query, repository.brandedQuery)
		}
	}
}

func TestBrandOnlyIntentFallsBackToOrdinaryWhenCredibleGenericExists(t *testing.T) {
	t.Parallel()
	match := &BrandMatch{Primary: "milk", Folded: "milk", Start: 0, End: 1}
	repository := &recordingRepository{
		brandMatch:        match,
		candidates:        []FoodCandidate{{FoodID: 1, Match: MatchMetadata{Class: MatchWord, Source: SourceCanonicalName}}},
		brandedCandidates: []FoodCandidate{{FoodID: 2, IsBranded: true}},
	}
	got, err := NewService(repository).Search(context.Background(), Request{Query: "milk"})
	if err != nil || len(got) != 1 || got[0].FoodID != 1 {
		t.Fatalf("ordinary collision result = %+v, %v", got, err)
	}
	if repository.brandedQuery.BrandOnly {
		t.Fatal("brand-only search ran despite credible generic food evidence")
	}
}

func TestBrandOnlyIntentUsesPersistedBrandCatalog(t *testing.T) {
	t.Parallel()
	match := &BrandMatch{Primary: "meijer", Folded: "meijer", Start: 0, End: 1}
	repository := &recordingRepository{
		brandMatch:        match,
		brandedCandidates: []FoodCandidate{{FoodID: 2, IsBranded: true}},
	}
	got, err := NewService(repository).Search(context.Background(), Request{Query: "meijer"})
	if err != nil || len(got) != 1 || got[0].FoodID != 2 || !repository.brandedQuery.BrandOnly {
		t.Fatalf("brand-only result = %+v, query=%+v, err=%v", got, repository.brandedQuery, err)
	}
}

func TestBrandPhraseExpansionIsContiguousAndLongestFirst(t *testing.T) {
	t.Parallel()
	phrases := brandPhrases(Normalize("milk Great Value"))
	if len(phrases) != 6 {
		t.Fatalf("phrase count = %d, want 6", len(phrases))
	}
	want := []string{"milk great value", "milk great", "great value", "milk", "great", "value"}
	for index := range want {
		if phrases[index].Primary != want[index] {
			t.Fatalf("phrase %d = %q, want %q", index, phrases[index].Primary, want[index])
		}
	}
}

func TestServiceValidatesAndBuildsRepositoryQuery(t *testing.T) {
	t.Parallel()
	repository := &recordingRepository{candidates: []FoodCandidate{{FoodID: 7}}}
	service := NewService(repository)
	got, err := service.Search(context.Background(), Request{Query: " SÜT ", Locale: "TR-tr"})
	if err != nil {
		t.Fatal(err)
	}
	wantQuery := Query{Primary: "süt", Folded: "sut", Locale: "tr-TR", BaseLocale: "tr", Limit: DefaultLimit}
	if repository.query != wantQuery {
		t.Fatalf("repository query = %+v, want %+v", repository.query, wantQuery)
	}
	if !reflect.DeepEqual(got, repository.candidates) {
		t.Fatalf("candidates = %+v", got)
	}
}

func TestServiceValidation(t *testing.T) {
	t.Parallel()
	tests := []Request{
		{},
		{Query: "   "},
		{Query: "a"},
		{Query: strings.Repeat("a", 121)},
		{Query: "milk", Locale: "tr_TR"},
		{Query: "milk", Locale: "t"},
		{Query: "milk", Limit: 0, LimitSet: true},
		{Query: "milk", Limit: -1, LimitSet: true},
		{Query: "milk", Limit: MaxLimit + 1, LimitSet: true},
	}
	service := NewService(&recordingRepository{})
	for _, request := range tests {
		if _, err := service.Search(context.Background(), request); !IsValidationError(err) {
			t.Errorf("Search(%+v) error = %v, want validation error", request, err)
		}
	}
}

func TestValidUnsupportedLocaleUsesCanonicalFallbackInputs(t *testing.T) {
	t.Parallel()
	repository := &recordingRepository{}
	_, err := NewService(repository).Search(context.Background(), Request{Query: "milch", Locale: "de-DE"})
	if err != nil {
		t.Fatal(err)
	}
	if repository.query.Locale != "de-DE" || repository.query.BaseLocale != "de" {
		t.Fatalf("locale input = %+v", repository.query)
	}
}

func TestServiceAcceptsMaximumLimit(t *testing.T) {
	t.Parallel()
	repository := &recordingRepository{}
	_, err := NewService(repository).Search(context.Background(), Request{
		Query: "milk", Limit: MaxLimit, LimitSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.query.Limit != MaxLimit {
		t.Fatalf("limit = %d, want %d", repository.query.Limit, MaxLimit)
	}
}
