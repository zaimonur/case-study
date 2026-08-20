// Package foodsearch defines multilingual canonical-food candidate retrieval.
package foodsearch

import "context"

const (
	DefaultLimit = 10
	MaxLimit     = 20
)

// Request is the public application input before validation and normalization.
type Request struct {
	Query    string
	Locale   string
	Limit    int
	LimitSet bool
}

// Query is the validated, deterministic repository input.
type Query struct {
	Primary    string
	Folded     string
	Locale     string
	BaseLocale string
	Limit      int
}

// MatchClass is the retrieval stage that produced a candidate.
type MatchClass uint8

const (
	MatchExact MatchClass = iota
	MatchWord
	MatchPrefix
	MatchFuzzy
)

// MatchSource identifies a retrieval signal without changing canonical identity.
type MatchSource uint8

const (
	SourceLocalizedDisplay MatchSource = iota
	SourceCanonicalName
	SourceLocalizationAlias
	SourceFoodAlias
	SourceBrand
)

// MatchForm records whether Turkish-preserving or ASCII-tolerant normalization matched.
type MatchForm uint8

const (
	FormPrimary MatchForm = iota
	FormFolded
)

// MatchMetadata is internal ranking/debug information for later candidate selection.
type MatchMetadata struct {
	Class      MatchClass
	Source     MatchSource
	Form       MatchForm
	Similarity float64
}

// FoodCandidate always represents one canonical foods.id row. Aliases are never identity.
type FoodCandidate struct {
	FoodID        int64
	CanonicalName string
	DisplayName   string
	Brand         *string
	// IsBranded is derived from stable product identity (currently GTIN/UPC), not nullable display metadata.
	IsBranded bool
	Match     MatchMetadata
}

// BrandPhrase is one bounded contiguous normalized phrase from the validated query.
type BrandPhrase struct {
	Primary    string
	Folded     string
	Start      int
	End        int
	TokenCount int
}

// BrandMatch identifies a query phrase proven against persisted brand data.
type BrandMatch struct {
	Primary string
	Folded  string
	Start   int
	End     int
}

// BrandedQuery searches a remaining product phrase only inside a persisted brand match.
type BrandedQuery struct {
	Query
	BrandPrimary string
	BrandFolded  string
	BrandOnly    bool
}

// Repository retrieves an ordered, bounded canonical candidate list.
type Repository interface {
	Search(context.Context, Query) ([]FoodCandidate, error)
	ResolveBrand(context.Context, []BrandPhrase) (*BrandMatch, error)
	SearchBranded(context.Context, BrandedQuery) ([]FoodCandidate, error)
}
