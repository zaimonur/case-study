// Package foodintent defines provider- and input-source-independent food intent data.
package foodintent

// FoodIntent is a normalized retrieval intent. UnitHint remains linguistic
// evidence and is not a resolved measurement.
type FoodIntent struct {
	Query    string
	Quantity *float64
	UnitHint *string
}
