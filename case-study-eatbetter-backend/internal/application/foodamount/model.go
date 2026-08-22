// Package foodamount deterministically resolves a trusted meal amount after
// canonical food identity has already been resolved.
package foodamount

import (
	"context"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/fooddetail"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

// State identifies whether an amount is trusted or requires clarification.
type State string

const (
	StateResolved              State = "resolved"
	StateClarificationRequired State = "clarification_required"
)

// Reason identifies the deterministic branch that produced the result.
type Reason string

const (
	ReasonExplicitGrams                        Reason = "explicit_grams"
	ReasonExplicitKilograms                    Reason = "explicit_kilograms"
	ReasonExplicitPortionSelection             Reason = "explicit_portion_selection"
	ReasonQuantityRequired                     Reason = "quantity_required"
	ReasonUnitRequired                         Reason = "unit_required"
	ReasonVolumeRequiresClarification          Reason = "volume_requires_clarification"
	ReasonUnsupportedUnitRequiresClarification Reason = "unsupported_unit_requires_clarification"
)

// SelectionKind identifies the trusted amount representation.
type SelectionKind string

const (
	SelectionGrams   SelectionKind = "grams"
	SelectionPortion SelectionKind = "portion"
)

// Request resolves an initial amount for an already-canonical food identity.
type Request struct {
	FoodID int64
	Intent foodintent.FoodIntent
	Locale string
}

// PortionSelectionRequest represents an explicit user selection of a stored portion.
type PortionSelectionRequest struct {
	FoodID    int64
	Locale    string
	PortionID int64
	Quantity  float64
}

// Resolution contains exactly one invariant-safe amount outcome.
type Resolution struct {
	State         State
	Reason        Reason
	Selection     *Selection
	Clarification *Clarification
}

// Selection contains trusted direct grams or a trusted stored-portion snapshot.
type Selection struct {
	Kind    SelectionKind
	FoodID  int64
	Grams   *GramsSelection
	Portion *PortionSelection
}

// GramsSelection is an explicit trusted mass in grams.
type GramsSelection struct {
	Grams float64
}

// PortionSelection combines user-derived quantity with persisted portion metadata.
type PortionSelection struct {
	PortionID    int64
	Quantity     float64
	Amount       float64
	Measure      string
	PortionGrams float64
}

// Clarification exposes persisted choices without choosing among them.
type Clarification struct {
	Portions         []food.Portion
	AllowDirectGrams bool
}

// DetailLoader is the narrow existing food-detail application boundary.
type DetailLoader interface {
	Get(context.Context, fooddetail.Request) (fooddetail.Detail, error)
}
