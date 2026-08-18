package food

import (
	"fmt"
	"math"
	"strings"
)

// Portion maps a free-form household measure to its gram weight.
type Portion struct {
	ID      int64
	FoodID  int64
	Amount  float64
	Measure string
	Grams   float64
}

// NewPortion creates a valid household portion.
func NewPortion(foodID int64, amount float64, measure string, grams float64) (Portion, error) {
	if !isFinitePositive(amount) {
		return Portion{}, fmt.Errorf("portion amount must be a finite positive number")
	}
	if !isFinitePositive(grams) {
		return Portion{}, fmt.Errorf("portion grams must be a finite positive number")
	}

	measure = strings.TrimSpace(measure)
	if measure == "" {
		return Portion{}, fmt.Errorf("portion measure must not be empty")
	}

	return Portion{
		FoodID:  foodID,
		Amount:  amount,
		Measure: measure,
		Grams:   grams,
	}, nil
}

func isFinitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}
