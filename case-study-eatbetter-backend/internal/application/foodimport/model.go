// Package foodimport defines provider-neutral records used to load canonical foods.
package foodimport

import (
	"context"
	"math"
)

const (
	// IdentifierSchemeGTINUPC is the canonical stable identity used for branded foods.
	IdentifierSchemeGTINUPC = "gtin_upc"
	// SourceUSDA identifies USDA-owned source record identifiers.
	SourceUSDA = "usda"
)

// Nutrients contains the canonical per-100-gram values selected by a source adapter.
// Nil means missing; a pointer to zero means known zero.
type Nutrients struct {
	Calories      *float64
	Protein       *float64
	Carbohydrates *float64
	Fat           *float64
}

// KnownCount reports how many canonical values are present.
func (n Nutrients) KnownCount() int {
	count := 0
	for _, value := range []*float64{n.Calories, n.Protein, n.Carbohydrates, n.Fat} {
		if value != nil {
			count++
		}
	}
	return count
}

// Equal compares missing values and exact source decimal values independently.
func (n Nutrients) Equal(other Nutrients) bool {
	return equalAmount(n.Calories, other.Calories) &&
		equalAmount(n.Protein, other.Protein) &&
		equalAmount(n.Carbohydrates, other.Carbohydrates) &&
		equalAmount(n.Fat, other.Fat)
}

func equalAmount(left, right *float64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return math.Float64bits(*left) == math.Float64bits(*right)
}

// Food is a fully mapped canonical food ready for persistence.
type Food struct {
	ImportKey     string
	GTIN          *string
	SelectedFDCID string
	CanonicalName string
	Brand         *string
	Nutrition     Nutrients
}

// Reference links a provider record to the canonical import identity.
type Reference struct {
	ImportKey  string
	ExternalID string
}

// Portion is a canonical gram-backed household measure.
type Portion struct {
	ImportKey string
	Amount    float64
	Measure   string
	Grams     float64
}

// MergeResult reports rows affected by the final canonical merge.
type MergeResult struct {
	Foods       int64
	Identifiers int64
	References  int64
	Nutrition   int64
	Portions    int64
}

// Stage receives bounded batches and atomically merges them into canonical storage.
type Stage interface {
	StageFoods(context.Context, []Food) error
	StageReferences(context.Context, []Reference) error
	StagePortions(context.Context, []Portion) error
	Commit(context.Context) (MergeResult, error)
	Rollback(context.Context) error
}

// StageFactory opens an isolated import staging session.
type StageFactory interface {
	Begin(context.Context) (Stage, error)
}
