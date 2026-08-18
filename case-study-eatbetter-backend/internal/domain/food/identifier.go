package food

import (
	"fmt"
	"strings"
)

// IdentifierScheme names a provider-neutral food identifier namespace.
type IdentifierScheme string

const (
	// IdentifierSchemeGTINUPC identifies retail products by their source-supplied GTIN or UPC.
	IdentifierSchemeGTINUPC IdentifierScheme = "gtin_upc"
)

// FoodIdentifier gives a canonical food a stable identity outside any one data provider.
type FoodIdentifier struct {
	ID     int64
	FoodID int64
	Scheme IdentifierScheme
	Value  string
}

// NewFoodIdentifier validates an identifier while preserving its exact textual representation.
// In particular, GTIN/UPC leading zeroes are significant and are never padded or parsed as a number.
func NewFoodIdentifier(foodID int64, scheme IdentifierScheme, value string) (FoodIdentifier, error) {
	if scheme != IdentifierSchemeGTINUPC {
		return FoodIdentifier{}, fmt.Errorf("unsupported food identifier scheme %q", scheme)
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return FoodIdentifier{}, fmt.Errorf("food identifier value must not be empty")
	}

	return FoodIdentifier{FoodID: foodID, Scheme: scheme, Value: value}, nil
}
