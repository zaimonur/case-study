// Package foodresolver deterministically resolves canonical food identity from
// the ordered candidates returned by the existing food search application.
package foodresolver

import (
	"context"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

// State identifies the deterministic identity-resolution outcome.
type State string

const (
	StateResolved  State = "resolved"
	StateAmbiguous State = "ambiguous"
	StateNotFound  State = "not_found"
)

// Reason explains which deterministic policy branch produced a resolution.
type Reason string

const (
	ReasonNoCandidates               Reason = "no_candidates"
	ReasonUniqueExactIdentity        Reason = "unique_exact_identity"
	ReasonMultipleExactIdentities    Reason = "multiple_exact_identities"
	ReasonNoSafeExactIdentity        Reason = "no_safe_exact_identity"
	ReasonCandidateSetMayBeTruncated Reason = "candidate_set_may_be_truncated"
)

// Request contains the provider-independent intent and caller-selected locale.
type Request struct {
	Intent foodintent.FoodIntent
	Locale string
}

// Resolution contains one invariant-safe identity decision and the original
// ordered candidate set.
type Resolution struct {
	State      State
	Reason     Reason
	Resolved   *foodsearch.FoodCandidate
	Candidates []foodsearch.FoodCandidate
}

// Searcher is the narrow existing food-search application boundary.
type Searcher interface {
	Search(context.Context, foodsearch.Request) ([]foodsearch.FoodCandidate, error)
}
