// Package mealai orchestrates provider-independent text meal interpretation.
package mealai

import (
	"context"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodamount"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodextraction"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodresolver"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

// State is the overall interpretation state.
type State string

const (
	StateReady                 State = "ready"
	StateClarificationRequired State = "clarification_required"
	StateEmpty                 State = "empty"
)

// ItemState is one extracted item's interpretation state.
type ItemState string

const (
	ItemReady                 ItemState = "ready"
	ItemClarificationRequired ItemState = "clarification_required"
)

// ClarificationKind identifies the unresolved application concern.
type ClarificationKind string

const (
	ClarificationFoodIdentity ClarificationKind = "food_identity"
	ClarificationAmount       ClarificationKind = "amount"
)

// Request is the initial text interpretation input.
type Request struct {
	Text   string
	Locale string
}

// Result contains ordered item interpretations.
type Result struct {
	State State
	Items []Item
}

// Item preserves source evidence and validated intent with its interpretation.
type Item struct {
	Mention       string
	Intent        foodintent.FoodIntent
	State         ItemState
	Food          *ResolvedFood
	Selection     *foodamount.Selection
	Clarification *Clarification
}

// ResolvedFood is a product-facing canonical identity snapshot.
type ResolvedFood struct {
	FoodID        int64
	DisplayName   string
	CanonicalName string
	Brand         *string
}

// FoodOption is a product-facing canonical identity choice.
type FoodOption struct {
	FoodID        int64
	DisplayName   string
	CanonicalName string
	Brand         *string
}

// Clarification is machine-readable unresolved state.
type Clarification struct {
	Kind             ClarificationKind
	Reason           string
	Candidates       []FoodOption
	Portions         []food.Portion
	AllowDirectGrams bool
}

// TextExtractor is the Task 1 application boundary.
type TextExtractor interface {
	Extract(context.Context, string) (foodextraction.TextFoodExtraction, error)
}

// FoodResolver is the Task 2 application boundary.
type FoodResolver interface {
	Resolve(context.Context, foodresolver.Request) (foodresolver.Resolution, error)
}

// AmountResolver is the Task 3 initial amount-resolution boundary.
type AmountResolver interface {
	Resolve(context.Context, foodamount.Request) (foodamount.Resolution, error)
}
