package bulkcsv

import (
	"context"
	"encoding/csv"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	appimport "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodimport"
)

func TestImporterMapsSyntheticDataset(t *testing.T) {
	t.Parallel()

	directory := writeSyntheticDataset(t)
	stage := &captureStage{}
	result, err := (Importer{
		DatasetDir:  directory,
		DatasetDate: mustDate(t, "2026-04-30"),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Stages:      captureFactory{stage: stage},
		BatchSize:   2,
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(stage.foods) != 4 {
		t.Fatalf("staged foods = %d, want 4: %+v", len(stage.foods), stage.foods)
	}
	branded := findFood(t, stage.foods, "gtin_upc:000000000001")
	if branded.SelectedFDCID != "101" || branded.GTIN == nil || *branded.GTIN != "000000000001" {
		t.Fatalf("unexpected branded identity: %+v", branded)
	}
	if branded.Brand == nil || *branded.Brand != "Example Brand" {
		t.Fatalf("branded Brand = %v, want Example Brand", branded.Brand)
	}
	assertAmount(t, branded.Nutrition.Calories, 110)
	assertAmount(t, branded.Nutrition.Protein, 2)

	foundation := findFood(t, stage.foods, "usda:200")
	assertAmount(t, foundation.Nutrition.Calories, 50)
	assertAmount(t, foundation.Nutrition.Protein, 0)
	if foundation.Nutrition.Carbohydrates != nil {
		t.Fatalf("negative carbohydrate mapped as known: %v", *foundation.Nutrition.Carbohydrates)
	}

	if !hasReference(stage.references, "gtin_upc:000000000001", "100") ||
		!hasReference(stage.references, "gtin_upc:000000000001", "101") {
		t.Fatalf("historical USDA references were not preserved: %+v", stage.references)
	}

	fnddsPortion := findPortion(t, stage.portions, "usda:201")
	if fnddsPortion.Amount != 1 || fnddsPortion.Measure != "1 cup" || fnddsPortion.Grams != 200 {
		t.Fatalf("FNDDS portion = %+v, want source-native phrase", fnddsPortion)
	}
	brandedPortion := findPortion(t, stage.portions, "gtin_upc:000000000001")
	if brandedPortion.Measure != "2 cookies" || brandedPortion.Grams != 30 {
		t.Fatalf("branded portion = %+v", brandedPortion)
	}
	if result.Diagnostics.InvalidNutrientAmounts != 1 || result.Diagnostics.OrphanPortions != 1 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
}

func TestNutritionPrecedenceAndKnownZero(t *testing.T) {
	t.Parallel()

	accumulator := nutrientAccumulator{
		energy:                candidateValue{value: 0, set: true},
		energyAtwaterSpecific: candidateValue{value: 20, set: true},
		energyAtwaterGeneral:  candidateValue{value: 30, set: true},
		protein:               candidateValue{value: 0, set: true},
	}
	nutrition := accumulator.canonical()
	assertAmount(t, nutrition.Calories, 0)
	assertAmount(t, nutrition.Protein, 0)
	if nutrition.Carbohydrates != nil || nutrition.Fat != nil || nutrition.KnownCount() != 2 {
		t.Fatalf("unexpected nutrition: %+v", nutrition)
	}

	accumulator.energy.set = false
	nutrition = accumulator.canonical()
	assertAmount(t, nutrition.Calories, 20)
	accumulator.energyAtwaterSpecific.set = false
	nutrition = accumulator.canonical()
	assertAmount(t, nutrition.Calories, 30)
}

func TestFoodAndPortionPolicies(t *testing.T) {
	t.Parallel()

	brand := selectBrand(" Consumer Brand ", "Owner Inc")
	if brand == nil || *brand != "Consumer Brand" {
		t.Fatalf("selectBrand() = %v", brand)
	}
	brand = selectBrand("", " Owner Inc ")
	if brand == nil || *brand != "Owner Inc" {
		t.Fatalf("selectBrand() owner fallback = %v", brand)
	}

	units := map[string]string{"1000": "cup", "9999": "undetermined"}
	foundation := make([]string, 11)
	foundation[3], foundation[4], foundation[5], foundation[6], foundation[7] = "1", "1000", "household note", "chopped", "50"
	portion, ok := mapGenericPortion("usda:1", dataTypeFoundation, foundation, units)
	if !ok || portion.Measure != "cup, chopped, household note" {
		t.Fatalf("foundation portion = %+v, %v", portion, ok)
	}

	fndds := make([]string, 11)
	fndds[4], fndds[5], fndds[6], fndds[7] = "9999", "2 slices, toasted", "12345", "64"
	portion, ok = mapGenericPortion("usda:2", dataTypeSurveyFNDDS, fndds, units)
	if !ok || portion.Amount != 1 || portion.Measure != "2 slices, toasted" {
		t.Fatalf("FNDDS portion = %+v, %v", portion, ok)
	}
	sr := make([]string, 11)
	sr[3], sr[4], sr[6], sr[7] = "2", "9999", "slice", "64"
	portion, ok = mapGenericPortion("usda:3", dataTypeSRLegacy, sr, units)
	if !ok || portion.Amount != 2 || portion.Measure != "slice" || portion.Grams != 64 {
		t.Fatalf("SR portion = %+v, %v", portion, ok)
	}

	diagnostics := Diagnostics{}
	if got := brandedPortion("gtin:1", "240", "ml", "1 cup", &diagnostics); got != nil {
		t.Fatalf("ml serving produced portion: %+v", got)
	}
	if got := brandedPortion("gtin:1", "30", "GRM", "1 serving", &diagnostics); got != nil {
		t.Fatalf("undocumented GRM serving produced portion: %+v", got)
	}
}

func TestDataTypeAllowlist(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"branded_food", "foundation_food", "survey_fndds_food", "sr_legacy_food"} {
		if _, importable, known := classifyDataType(value); !known || !importable {
			t.Fatalf("classifyDataType(%q) = importable %v known %v", value, importable, known)
		}
	}
	for _, value := range []string{"sample_food", "sub_sample_food", "market_acquistion", "agricultural_acquisition", "experimental_food"} {
		if _, importable, known := classifyDataType(value); !known || importable {
			t.Fatalf("classifyDataType(%q) = importable %v known %v", value, importable, known)
		}
	}
	if _, _, known := classifyDataType("future_unknown_type"); known {
		t.Fatal("unknown data type was silently accepted")
	}
}

func TestBrandedTieSelectionAndDiscontinuedExclusion(t *testing.T) {
	t.Parallel()

	stage := &captureStage{}
	_, err := (Importer{
		DatasetDir:  writeBrandedDecisionDataset(t, []string{"Same Product", "Same Product"}, ""),
		DatasetDate: mustDate(t, "2026-04-30"),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Stages:      captureFactory{stage: stage},
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("equal-payload tie: %v", err)
	}
	if len(stage.foods) != 1 || stage.foods[0].SelectedFDCID != "100" {
		t.Fatalf("tie selected %+v, want smallest FDC ID 100", stage.foods)
	}

	stage = &captureStage{}
	result, err := (Importer{
		DatasetDir:  writeBrandedDecisionDataset(t, []string{"Discontinued Product"}, "2026-04-30"),
		DatasetDate: mustDate(t, "2026-04-30"),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Stages:      captureFactory{stage: stage},
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("discontinued selection: %v", err)
	}
	if len(stage.foods) != 0 || result.Diagnostics.DiscontinuedBrandedFoods != 1 {
		t.Fatalf("discontinued food staged: foods=%+v diagnostics=%+v", stage.foods, result.Diagnostics)
	}
}

func TestBrandedTieWithDifferentPayloadFails(t *testing.T) {
	t.Parallel()

	_, err := (Importer{
		DatasetDir:  writeBrandedDecisionDataset(t, []string{"Payload A", "Payload B"}, ""),
		DatasetDate: mustDate(t, "2026-04-30"),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Stages:      captureFactory{stage: &captureStage{}},
	}).Run(context.Background())
	if err == nil {
		t.Fatal("different-payload latest tie succeeded")
	}
}

func TestVersionKeyOrderAndPayloadEquality(t *testing.T) {
	t.Parallel()

	if got := (versionKey{publication: 20260101}).Compare(versionKey{publication: 20250101, modified: 99999999}); got != 1 {
		t.Fatalf("publication precedence compare = %d, want 1", got)
	}
	if got := (versionKey{publication: 20260101, modified: 20260201}).Compare(versionKey{publication: 20260101, modified: 20260101, available: 99999999}); got != 1 {
		t.Fatalf("modified precedence compare = %d, want 1", got)
	}

	zero := 0.0
	left := brandCandidate{fdcID: 10, canonicalName: "Food", nutrition: appimport.Nutrients{Protein: &zero}}
	right := brandCandidate{fdcID: 11, canonicalName: "Food", nutrition: appimport.Nutrients{Protein: &zero}}
	if !left.samePayload(right) {
		t.Fatal("same canonical payload differs only by FDC ID")
	}
	right.canonicalName = "Other"
	if left.samePayload(right) {
		t.Fatal("different canonical payload compared equal")
	}
}

func TestParseDateValidatesCalendarDate(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"2026-13-01", "2026-04-31", "2025-02-29"} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, err := parseDate(raw, "test.date", false); err == nil {
				t.Fatalf("parseDate(%q) error = nil, want invalid calendar date error", raw)
			}
		})
	}

	value, err := parseDate("2024-02-29", "test.date", false)
	if err != nil {
		t.Fatalf("parseDate(valid leap day) error = %v", err)
	}
	if value != 20240229 {
		t.Fatalf("parseDate(valid leap day) = %d, want 20240229", value)
	}
}

func TestHeaderValidationFailsClosed(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeCSV(t, directory, "nutrient.csv", []string{"id", "renamed"}, [][]string{{"1008", "Energy"}})
	importer := Importer{DatasetDir: directory, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := importer.validateNutrients(context.Background()); err == nil {
		t.Fatal("validateNutrients() error = nil, want header contract error")
	}
}

type captureFactory struct{ stage *captureStage }

func (f captureFactory) Begin(context.Context) (appimport.Stage, error) { return f.stage, nil }

type captureStage struct {
	foods      []appimport.Food
	references []appimport.Reference
	portions   []appimport.Portion
}

func (s *captureStage) StageFoods(_ context.Context, rows []appimport.Food) error {
	s.foods = append(s.foods, slices.Clone(rows)...)
	return nil
}
func (s *captureStage) StageReferences(_ context.Context, rows []appimport.Reference) error {
	s.references = append(s.references, slices.Clone(rows)...)
	return nil
}
func (s *captureStage) StagePortions(_ context.Context, rows []appimport.Portion) error {
	s.portions = append(s.portions, slices.Clone(rows)...)
	return nil
}
func (s *captureStage) Commit(context.Context) (appimport.MergeResult, error) {
	return appimport.MergeResult{Foods: int64(len(s.foods)), References: int64(len(s.references)), Portions: int64(len(s.portions))}, nil
}
func (s *captureStage) Rollback(context.Context) error { return nil }

func writeSyntheticDataset(t *testing.T) string {
	return writeSyntheticDatasetVersion(t, true)
}

func writeSyntheticDatasetVersion(t *testing.T, includeCurrentVersion bool) string {
	t.Helper()
	directory := t.TempDir()
	writeCSV(t, directory, "nutrient.csv", nutrientHeader, [][]string{
		{"1008", "Energy", "KCAL", "208", "300"},
		{"2048", "Energy (Atwater Specific Factors)", "KCAL", "958", "290"},
		{"2047", "Energy (Atwater General Factors)", "KCAL", "957", "280"},
		{"1003", "Protein", "G", "203", "600"},
		{"1005", "Carbohydrate, by difference", "G", "205", "1110"},
		{"1004", "Total lipid (fat)", "G", "204", "800"},
	})
	writeCSV(t, directory, "measure_unit.csv", measureUnitHeader, [][]string{{"1000", "cup"}, {"9999", "undetermined"}})
	foodRows := [][]string{
		{"100", "branded_food", "Old Product", "Snacks", "2025-01-01"},
		{"200", "foundation_food", "Foundation Food", "1", "2026-01-01"},
		{"201", "survey_fndds_food", "Survey Food", "1000", "2026-01-01"},
		{"202", "sr_legacy_food", "Legacy Food", "1", "2019-04-01"},
		{"300", "sample_food", "Lab Sample", "1", "7/19/2023"},
	}
	brandedRows := [][]string{
		brandedRow("100", "Owner Inc", "", "000000000001", "240", "ml", "1 cup", "2024-12-01", "2025-01-01", ""),
	}
	nutrientRows := [][]string{
		nutrientRow("1", "100", "1008", "100"), nutrientRow("2", "100", "1003", "1"),
		nutrientRow("8", "200", "2048", "50"), nutrientRow("9", "200", "1003", "0"), nutrientRow("10", "200", "1005", "-1"), nutrientRow("11", "200", "1004", "1"),
		nutrientRow("12", "201", "1008", "80"), nutrientRow("13", "202", "1008", "90"),
	}
	if includeCurrentVersion {
		foodRows = append(foodRows, []string{"101", "branded_food", "Current Product", "Snacks", "2026-01-01"})
		brandedRows = append(brandedRows, brandedRow("101", "Owner Inc", "Example Brand", "000000000001", "30", "g", "2 cookies", "2025-12-01", "2026-01-01", ""))
		nutrientRows = append(nutrientRows,
			nutrientRow("3", "101", "1008", "110"), nutrientRow("4", "101", "2048", "999"), nutrientRow("5", "101", "1003", "2"),
			nutrientRow("6", "101", "1005", "15"), nutrientRow("7", "101", "1004", "4"),
		)
	}
	writeCSV(t, directory, "food.csv", foodHeader, foodRows)
	writeCSV(t, directory, "branded_food.csv", brandedFoodHeader, brandedRows)
	writeCSV(t, directory, "food_nutrient.csv", foodNutrientHeader, nutrientRows)
	writeCSV(t, directory, "food_portion.csv", foodPortionHeader, [][]string{
		portionRow("1", "200", "1", "1000", "", "chopped", "50"),
		portionRow("2", "201", "", "9999", "1 cup", "10205", "200"),
		portionRow("3", "202", "1", "9999", "", "slice", "30"),
		portionRow("4", "", "1", "", "", "", "40"),
	})
	return directory
}

func writeBrandedDecisionDataset(t *testing.T, descriptions []string, discontinued string) string {
	t.Helper()
	directory := t.TempDir()
	writeCSV(t, directory, "nutrient.csv", nutrientHeader, [][]string{
		{"1008", "Energy", "KCAL", "208", "300"},
		{"2048", "Energy (Atwater Specific Factors)", "KCAL", "958", "290"},
		{"2047", "Energy (Atwater General Factors)", "KCAL", "957", "280"},
		{"1003", "Protein", "G", "203", "600"},
		{"1005", "Carbohydrate, by difference", "G", "205", "1110"},
		{"1004", "Total lipid (fat)", "G", "204", "800"},
	})
	writeCSV(t, directory, "measure_unit.csv", measureUnitHeader, [][]string{{"9999", "undetermined"}})
	foodRows := make([][]string, 0, len(descriptions))
	brandedRows := make([][]string, 0, len(descriptions))
	nutrientRows := make([][]string, 0, len(descriptions))
	for index, description := range descriptions {
		fdcID := "100"
		if index == 1 {
			fdcID = "101"
		}
		foodRows = append(foodRows, []string{fdcID, "branded_food", description, "Snacks", "2026-01-01"})
		brandedRows = append(brandedRows, brandedRow(fdcID, "Owner", "Brand", "000000000009", "", "", "", "2026-01-01", "2026-01-01", discontinued))
		nutrientRows = append(nutrientRows, nutrientRow(fdcID, fdcID, "1008", "100"))
	}
	writeCSV(t, directory, "food.csv", foodHeader, foodRows)
	writeCSV(t, directory, "branded_food.csv", brandedFoodHeader, brandedRows)
	writeCSV(t, directory, "food_nutrient.csv", foodNutrientHeader, nutrientRows)
	writeCSV(t, directory, "food_portion.csv", foodPortionHeader, nil)
	return directory
}

func brandedRow(fdcID, owner, name, gtin, size, unit, household, modified, available, discontinued string) []string {
	row := make([]string, len(brandedFoodHeader))
	row[0], row[1], row[2], row[4] = fdcID, owner, name, gtin
	row[7], row[8], row[9] = size, unit, household
	row[13], row[14], row[15], row[16] = modified, available, "United States", discontinued
	return row
}

func nutrientRow(id, fdcID, nutrientID, amount string) []string {
	row := make([]string, len(foodNutrientHeader))
	row[0], row[1], row[2], row[3] = id, fdcID, nutrientID, amount
	return row
}

func portionRow(id, fdcID, amount, unitID, description, modifier, grams string) []string {
	row := make([]string, len(foodPortionHeader))
	row[0], row[1], row[3], row[4], row[5], row[6], row[7] = id, fdcID, amount, unitID, description, modifier, grams
	return row
}

func writeCSV(t *testing.T, directory, name string, header []string, rows [][]string) {
	t.Helper()
	file, err := os.Create(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	writer := csv.NewWriter(file)
	if err := writer.Write(header); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteAll(rows); err != nil {
		t.Fatal(err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustDate(t *testing.T, raw string) time.Time {
	t.Helper()
	value, err := time.Parse("2006-01-02", raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func findFood(t *testing.T, foods []appimport.Food, key string) appimport.Food {
	t.Helper()
	for _, food := range foods {
		if food.ImportKey == key {
			return food
		}
	}
	t.Fatalf("food %q not found in %+v", key, foods)
	return appimport.Food{}
}

func findPortion(t *testing.T, portions []appimport.Portion, key string) appimport.Portion {
	t.Helper()
	for _, portion := range portions {
		if portion.ImportKey == key {
			return portion
		}
	}
	t.Fatalf("portion %q not found in %+v", key, portions)
	return appimport.Portion{}
}

func hasReference(references []appimport.Reference, key, externalID string) bool {
	return slices.ContainsFunc(references, func(reference appimport.Reference) bool {
		return reference.ImportKey == key && reference.ExternalID == externalID
	})
}

func assertAmount(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("amount = %v, want %v", got, want)
	}
}
