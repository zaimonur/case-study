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
	Match         MatchMetadata
}

// Repository retrieves an ordered, bounded canonical candidate list.
type Repository interface {
	Search(context.Context, Query) ([]FoodCandidate, error)
}
