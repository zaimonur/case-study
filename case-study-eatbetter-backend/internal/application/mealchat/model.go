// Package mealchat defines and validates provider-independent conversational
// interpretation contracts. It never resolves canonical food or nutrition.
package mealchat

import (
	"context"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodintent"
	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

const (
	MaxMessageRunes = 2000
	MaxItems        = 12
)

// Purpose distinguishes supported product intents without changing the
// existing consumed-meal extractor semantics.
type Purpose string

const (
	PurposeMealLogging    Purpose = "meal_logging"
	PurposeNutritionQuery Purpose = "nutrition_query"
	PurposeUnknown        Purpose = "unknown"
)

// InitialInterpretation is the bounded result of a chat-specific first turn.
type InitialInterpretation struct {
	Purpose Purpose
	Items   []InitialItem
}

// InitialItem preserves exact source evidence and a reusable food intent.
type InitialItem struct {
	Evidence string
	Intent   foodintent.FoodIntent
}

// ClarificationKind describes the only contexts visible to a continuation
// interpreter.
type ClarificationKind string

const (
	ClarificationFoodIdentity ClarificationKind = "food_identity"
	ClarificationAmount       ClarificationKind = "amount"
)

// FoodCandidate is an allowed identity option. Its text fields are data only.
type FoodCandidate struct {
	FoodID        int64
	DisplayName   string
	CanonicalName string
	Brand         *string
}

// ResolvedFood is the current trusted identity context for amount questions.
type ResolvedFood struct {
	FoodID        int64
	DisplayName   string
	CanonicalName string
	Brand         *string
}

// ContinuationRequest scopes the latest message to one current clarification.
type ContinuationRequest struct {
	Message          string
	Kind             ClarificationKind
	OriginalEvidence string
	OriginalIntent   foodintent.FoodIntent
	ResolvedFood     *ResolvedFood
	Candidates       []FoodCandidate
	Portions         []food.Portion
}

// ContinuationKind is a constrained provider decision or an unresolved result.
type ContinuationKind string

const (
	ContinuationUnresolved   ContinuationKind = "unresolved"
	ContinuationFoodIdentity ContinuationKind = "food_identity"
	ContinuationGrams        ContinuationKind = "grams"
	ContinuationPortion      ContinuationKind = "portion"
)

// ContinuationDecision contains only fields allowed by Kind.
type ContinuationDecision struct {
	Kind      ContinuationKind
	FoodID    *int64
	Grams     *float64
	PortionID *int64
	Quantity  *float64
}

// Interpreter is the replaceable provider boundary.
type Interpreter interface {
	InterpretInitial(context.Context, string) (InitialInterpretation, error)
	InterpretContinuation(context.Context, ContinuationRequest) (ContinuationDecision, error)
}
