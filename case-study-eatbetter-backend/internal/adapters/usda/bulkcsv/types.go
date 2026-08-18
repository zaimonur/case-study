package bulkcsv

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	appimport "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodimport"
)

const (
	dataTypeBranded uint8 = iota + 1
	dataTypeFoundation
	dataTypeSurveyFNDDS
	dataTypeSRLegacy
)

var knownExcludedDataTypes = map[string]struct{}{
	"sample_food":              {},
	"sub_sample_food":          {},
	"market_acquistion":        {},
	"agricultural_acquisition": {},
	"experimental_food":        {},
}

type foodIndexEntry struct {
	publicationDate int32
	dataType        uint8
	present         bool
}

type genericFood struct {
	fdcID       int64
	description string
	dataType    uint8
	nutrition   appimport.Nutrients
}

type versionKey struct {
	publication int32
	modified    int32
	available   int32
}

func (k versionKey) Compare(other versionKey) int {
	for _, pair := range [][2]int32{{k.publication, other.publication}, {k.modified, other.modified}, {k.available, other.available}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	return 0
}

type brandGroup struct {
	gtin       string
	version    versionKey
	primaryID  int64
	tiedIDs    []int64
	candidates []brandCandidate
	eligible   bool
}

func (g *brandGroup) versionIDs() []int64 {
	ids := make([]int64, 1, 1+len(g.tiedIDs))
	ids[0] = g.primaryID
	return append(ids, g.tiedIDs...)
}

type brandCandidate struct {
	fdcID            int64
	canonicalName    string
	brand            *string
	nutrition        appimport.Nutrients
	portion          *appimport.Portion
	discontinuedDate int32
}

func (c brandCandidate) samePayload(other brandCandidate) bool {
	return c.canonicalName == other.canonicalName &&
		equalStringPointer(c.brand, other.brand) &&
		c.nutrition.Equal(other.nutrition) &&
		equalPortion(c.portion, other.portion) &&
		c.discontinuedDate == other.discontinuedDate
}

func equalStringPointer(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalPortion(left, right *appimport.Portion) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return math.Float64bits(left.Amount) == math.Float64bits(right.Amount) &&
		left.Measure == right.Measure &&
		math.Float64bits(left.Grams) == math.Float64bits(right.Grams)
}

type candidateValue struct {
	value float64
	set   bool
}

type nutrientAccumulator struct {
	energy                candidateValue
	energyAtwaterSpecific candidateValue
	energyAtwaterGeneral  candidateValue
	protein               candidateValue
	carbohydrates         candidateValue
	fat                   candidateValue
}

func (a nutrientAccumulator) canonical() appimport.Nutrients {
	return appimport.Nutrients{
		Calories:      firstAmount(a.energy, a.energyAtwaterSpecific, a.energyAtwaterGeneral),
		Protein:       amountPointer(a.protein),
		Carbohydrates: amountPointer(a.carbohydrates),
		Fat:           amountPointer(a.fat),
	}
}

func firstAmount(values ...candidateValue) *float64 {
	for _, value := range values {
		if value.set {
			copy := value.value
			return &copy
		}
	}
	return nil
}

func amountPointer(value candidateValue) *float64 {
	if !value.set {
		return nil
	}
	copy := value.value
	return &copy
}

func parsePositiveID(raw, field string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", field, raw)
	}
	return value, nil
}

func parseDate(raw, field string, optional bool) (int32, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" && optional {
		return 0, nil
	}
	if len(raw) != len("2006-01-02") || raw[4] != '-' || raw[7] != '-' {
		return 0, fmt.Errorf("%s must use YYYY-MM-DD, got %q", field, raw)
	}
	compact := raw[:4] + raw[5:7] + raw[8:]
	value, err := strconv.ParseInt(compact, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s %q: %w", field, raw, err)
	}
	return int32(value), nil
}

func parseFinite(raw, field string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s must be a finite number, got %q", field, raw)
	}
	return value, nil
}

func importKeyForGTIN(gtin string) string { return "gtin_upc:" + gtin }

func importKeyForFDC(fdcID int64) string { return "usda:" + strconv.FormatInt(fdcID, 10) }
