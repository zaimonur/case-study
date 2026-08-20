// Package fooddetail defines canonical food detail retrieval for product clients.
package fooddetail

import (
	"context"
	"errors"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

// ErrNotFound means the canonical food identity does not exist.
var ErrNotFound = errors.New("food not found")

// Request is the unvalidated detail request.
type Request struct {
	FoodID int64
	Locale string
}

// Query is the validated repository input.
type Query struct {
	FoodID     int64
	Locale     string
	BaseLocale string
}

// Detail contains only product-facing canonical data.
type Detail struct {
	Food        food.Food
	DisplayName string
	Nutrition   *food.Nutrition
	Portions    []food.Portion
}

// Repository loads one canonical food without provider-specific metadata.
type Repository interface {
	Get(context.Context, Query) (Detail, error)
}
