// Package bulkcsv imports the USDA FoodData Central bulk CSV release without exposing
// provider-specific DTOs to the canonical food domain.
package bulkcsv

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	appimport "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodimport"
)

var (
	foodHeader        = []string{"fdc_id", "data_type", "description", "food_category_id", "publication_date"}
	brandedFoodHeader = []string{
		"fdc_id", "brand_owner", "brand_name", "subbrand_name", "gtin_upc", "ingredients",
		"not_a_significant_source_of", "serving_size", "serving_size_unit", "household_serving_fulltext",
		"branded_food_category", "data_source", "package_weight", "modified_date", "available_date",
		"market_country", "discontinued_date", "preparation_state_code", "trade_channel", "short_description",
		"material_code",
	}
	foodNutrientHeader = []string{
		"id", "fdc_id", "nutrient_id", "amount", "data_points", "derivation_id", "min", "max", "median",
		"loq", "footnote", "min_year_acquired", "percent_daily_value",
	}
	nutrientHeader    = []string{"id", "name", "unit_name", "nutrient_nbr", "rank"}
	foodPortionHeader = []string{
		"id", "fdc_id", "seq_num", "amount", "measure_unit_id", "portion_description", "modifier",
		"gram_weight", "data_points", "footnote", "min_year_acquired",
	}
	measureUnitHeader = []string{"id", "name"}
)

const defaultBatchSize = 10_000

// Diagnostics records intentional source-data rejections separately from fatal contract errors.
type Diagnostics struct {
	FoodRows                   int64
	BrandedRows                int64
	NutrientRows               int64
	PortionRows                int64
	ExcludedDataTypeRows       int64
	BlankGTINRows              int64
	InvalidNutrientAmounts     int64
	DuplicateNutrientRows      int64
	FoodsWithoutNutrition      int64
	DiscontinuedBrandedFoods   int64
	InvalidPortions            int64
	OrphanPortions             int64
	UnsupportedBrandedServings int64
	SelectedBrandedFoods       int64
	SelectedGenericFoods       int64
	HistoricalUSDAReferences   int64
}

// Result summarizes a complete USDA import.
type Result struct {
	Diagnostics Diagnostics
	Merged      appimport.MergeResult
	Duration    time.Duration
}

// Importer orchestrates bounded-memory CSV parsing and atomic canonical persistence.
type Importer struct {
	DatasetDir  string
	DatasetDate time.Time
	Logger      *slog.Logger
	Stages      appimport.StageFactory
	BatchSize   int
}

// Run validates and imports a FoodData Central bulk CSV release.
func (i Importer) Run(ctx context.Context) (result Result, err error) {
	started := time.Now()
	if strings.TrimSpace(i.DatasetDir) == "" {
		return result, fmt.Errorf("dataset directory is required")
	}
	if i.DatasetDate.IsZero() {
		return result, fmt.Errorf("dataset date is required")
	}
	if i.Stages == nil {
		return result, fmt.Errorf("stage factory is required")
	}
	if i.BatchSize <= 0 {
		i.BatchSize = defaultBatchSize
	}
	if i.Logger == nil {
		i.Logger = slog.Default()
	}

	if err := i.validateRequiredHeaders(); err != nil {
		return result, err
	}
	if err := i.validateNutrients(ctx); err != nil {
		return result, err
	}
	measureUnits, err := i.loadMeasureUnits(ctx)
	if err != nil {
		return result, err
	}

	foodIndex, genericFoods, err := i.loadFoodIndex(ctx, &result.Diagnostics)
	if err != nil {
		return result, err
	}
	brandGroups, err := i.selectBrandedVersions(ctx, foodIndex, &result.Diagnostics)
	if err != nil {
		return result, err
	}

	accumulators := make(map[int64]nutrientAccumulator, len(genericFoods)+len(brandGroups))
	for fdcID := range genericFoods {
		accumulators[fdcID] = nutrientAccumulator{}
	}
	for _, group := range brandGroups {
		for _, fdcID := range group.versionIDs() {
			accumulators[fdcID] = nutrientAccumulator{}
		}
	}
	if err := i.loadNutrition(ctx, accumulators, &result.Diagnostics); err != nil {
		return result, err
	}

	for fdcID, food := range genericFoods {
		food.nutrition = accumulators[fdcID].canonical()
		if food.nutrition.KnownCount() == 0 {
			delete(genericFoods, fdcID)
			result.Diagnostics.FoodsWithoutNutrition++
		}
	}
	for _, group := range brandGroups {
		maxKnown := 0
		ids := group.versionIDs()
		for _, fdcID := range ids {
			maxKnown = max(maxKnown, accumulators[fdcID].canonical().KnownCount())
		}
		if maxKnown == 0 {
			group.eligible = false
			result.Diagnostics.FoodsWithoutNutrition++
			continue
		}
		group.eligible = true
		group.tiedIDs = group.tiedIDs[:0]
		group.primaryID = 0
		for _, fdcID := range ids {
			if accumulators[fdcID].canonical().KnownCount() != maxKnown {
				continue
			}
			if group.primaryID == 0 {
				group.primaryID = fdcID
			} else {
				group.tiedIDs = append(group.tiedIDs, fdcID)
			}
		}
	}

	descriptions, err := i.loadCandidateDescriptions(ctx, brandGroups)
	if err != nil {
		return result, err
	}

	stage, err := i.Stages.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin USDA import stage: %w", err)
	}
	defer func() {
		if err != nil {
			_ = stage.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if err = i.stageBranded(ctx, stage, foodIndex, brandGroups, descriptions, accumulators, &result.Diagnostics); err != nil {
		return result, err
	}
	if err = i.stageGeneric(ctx, stage, genericFoods, &result.Diagnostics); err != nil {
		return result, err
	}
	if err = i.stageGenericPortions(ctx, stage, genericFoods, measureUnits, &result.Diagnostics); err != nil {
		return result, err
	}

	result.Merged, err = stage.Commit(ctx)
	if err != nil {
		return result, fmt.Errorf("commit USDA canonical merge: %w", err)
	}
	result.Duration = time.Since(started)
	i.Logger.Info("USDA bulk import complete",
		"duration", result.Duration.Round(time.Millisecond),
		"selected_branded_foods", result.Diagnostics.SelectedBrandedFoods,
		"selected_generic_foods", result.Diagnostics.SelectedGenericFoods,
		"historical_usda_references", result.Diagnostics.HistoricalUSDAReferences,
		"foods", result.Merged.Foods,
		"nutrition", result.Merged.Nutrition,
		"portions", result.Merged.Portions,
	)
	return result, nil
}

func (i Importer) validateRequiredHeaders() error {
	required := []struct {
		name   string
		header []string
	}{
		{"food.csv", foodHeader},
		{"branded_food.csv", brandedFoodHeader},
		{"food_nutrient.csv", foodNutrientHeader},
		{"nutrient.csv", nutrientHeader},
		{"food_portion.csv", foodPortionHeader},
		{"measure_unit.csv", measureUnitHeader},
	}
	for _, file := range required {
		scanner, err := openCSV(i.DatasetDir, file.name, file.header, nil)
		if err != nil {
			return err
		}
		if err := scanner.file.Close(); err != nil {
			return fmt.Errorf("close %s after header validation: %w", file.name, err)
		}
	}
	return nil
}

func (i Importer) validateNutrients(ctx context.Context) error {
	wanted := map[string][2]string{
		"1008": {"Energy", "KCAL"},
		"2048": {"Energy (Atwater Specific Factors)", "KCAL"},
		"2047": {"Energy (Atwater General Factors)", "KCAL"},
		"1003": {"Protein", "G"},
		"1005": {"Carbohydrate, by difference", "G"},
		"1004": {"Total lipid (fat)", "G"},
	}
	found := make(map[string]struct{}, len(wanted))
	scanner, err := openCSV(i.DatasetDir, "nutrient.csv", nutrientHeader, i.Logger)
	if err != nil {
		return err
	}
	if err := scanner.Scan(ctx, func(record []string) error {
		expected, ok := wanted[record[0]]
		if !ok {
			return nil
		}
		if record[1] != expected[0] || record[2] != expected[1] {
			return fmt.Errorf("nutrient %s changed: got name=%q unit=%q, want name=%q unit=%q", record[0], record[1], record[2], expected[0], expected[1])
		}
		if _, duplicate := found[record[0]]; duplicate {
			return fmt.Errorf("duplicate nutrient dictionary ID %s", record[0])
		}
		found[record[0]] = struct{}{}
		return nil
	}); err != nil {
		return err
	}
	if len(found) != len(wanted) {
		missing := make([]string, 0)
		for id := range wanted {
			if _, ok := found[id]; !ok {
				missing = append(missing, id)
			}
		}
		slices.Sort(missing)
		return fmt.Errorf("nutrient dictionary is missing required IDs %v", missing)
	}
	return nil
}

func (i Importer) loadMeasureUnits(ctx context.Context) (map[string]string, error) {
	units := make(map[string]string)
	scanner, err := openCSV(i.DatasetDir, "measure_unit.csv", measureUnitHeader, i.Logger)
	if err != nil {
		return nil, err
	}
	if err := scanner.Scan(ctx, func(record []string) error {
		id := strings.TrimSpace(record[0])
		name := strings.TrimSpace(record[1])
		if id == "" || name == "" {
			return fmt.Errorf("measure unit ID and name must not be blank")
		}
		if _, duplicate := units[id]; duplicate {
			return fmt.Errorf("duplicate measure unit ID %s", id)
		}
		units[id] = name
		return nil
	}); err != nil {
		return nil, err
	}
	if units["9999"] != "undetermined" {
		return nil, fmt.Errorf("measure unit 9999 changed: got %q, want %q", units["9999"], "undetermined")
	}
	return units, nil
}

func (i Importer) loadFoodIndex(ctx context.Context, diagnostics *Diagnostics) ([]foodIndexEntry, map[int64]*genericFood, error) {
	index := make([]foodIndexEntry, 1)
	generic := make(map[int64]*genericFood)
	scanner, err := openCSV(i.DatasetDir, "food.csv", foodHeader, i.Logger)
	if err != nil {
		return nil, nil, err
	}
	err = scanner.Scan(ctx, func(record []string) error {
		diagnostics.FoodRows++
		fdcID, err := parsePositiveID(record[0], "food.fdc_id")
		if err != nil {
			return err
		}
		dataType, importable, known := classifyDataType(record[1])
		if !known {
			return fmt.Errorf("unknown food data_type %q", record[1])
		}
		if fdcID >= int64(len(index)) {
			index = slices.Grow(index, int(fdcID)-len(index)+1)
			index = index[:fdcID+1]
		}
		if index[fdcID].present {
			return fmt.Errorf("duplicate food.fdc_id %d", fdcID)
		}
		index[fdcID] = foodIndexEntry{dataType: dataType, present: true}
		if !importable {
			diagnostics.ExcludedDataTypeRows++
			return nil
		}
		publicationDate, err := parseDate(record[4], "food.publication_date", false)
		if err != nil {
			return err
		}
		index[fdcID].publicationDate = publicationDate
		if dataType == dataTypeBranded {
			return nil
		}
		description := strings.TrimSpace(record[2])
		if description == "" {
			return fmt.Errorf("food %d has a blank description", fdcID)
		}
		generic[fdcID] = &genericFood{fdcID: fdcID, description: strings.Clone(description), dataType: dataType}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return index, generic, nil
}

func classifyDataType(raw string) (dataType uint8, importable, known bool) {
	switch raw {
	case "branded_food":
		return dataTypeBranded, true, true
	case "foundation_food":
		return dataTypeFoundation, true, true
	case "survey_fndds_food":
		return dataTypeSurveyFNDDS, true, true
	case "sr_legacy_food":
		return dataTypeSRLegacy, true, true
	default:
		_, known = knownExcludedDataTypes[raw]
		return 0, false, known
	}
}

func (i Importer) selectBrandedVersions(ctx context.Context, foodIndex []foodIndexEntry, diagnostics *Diagnostics) (map[string]*brandGroup, error) {
	groups := make(map[string]*brandGroup)
	scanner, err := openCSV(i.DatasetDir, "branded_food.csv", brandedFoodHeader, i.Logger)
	if err != nil {
		return nil, err
	}
	err = scanner.Scan(ctx, func(record []string) error {
		diagnostics.BrandedRows++
		fdcID, err := parsePositiveID(record[0], "branded_food.fdc_id")
		if err != nil {
			return err
		}
		if fdcID >= int64(len(foodIndex)) || foodIndex[fdcID].dataType != dataTypeBranded {
			return fmt.Errorf("branded food %d does not reference a branded food row", fdcID)
		}
		gtin := strings.TrimSpace(record[4])
		if gtin == "" {
			diagnostics.BlankGTINRows++
			return nil
		}
		modified, err := parseDate(record[13], "branded_food.modified_date", true)
		if err != nil {
			return err
		}
		available, err := parseDate(record[14], "branded_food.available_date", false)
		if err != nil {
			return err
		}
		key := versionKey{publication: foodIndex[fdcID].publicationDate, modified: modified, available: available}
		group, exists := groups[gtin]
		if !exists {
			groups[strings.Clone(gtin)] = &brandGroup{gtin: strings.Clone(gtin), version: key, primaryID: fdcID}
			return nil
		}
		switch key.Compare(group.version) {
		case 1:
			group.version = key
			group.primaryID = fdcID
			group.tiedIDs = group.tiedIDs[:0]
		case 0:
			group.tiedIDs = append(group.tiedIDs, fdcID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func (i Importer) loadNutrition(ctx context.Context, accumulators map[int64]nutrientAccumulator, diagnostics *Diagnostics) error {
	scanner, err := openCSV(i.DatasetDir, "food_nutrient.csv", foodNutrientHeader, i.Logger)
	if err != nil {
		return err
	}
	return scanner.Scan(ctx, func(record []string) error {
		diagnostics.NutrientRows++
		fdcID, err := parsePositiveID(record[1], "food_nutrient.fdc_id")
		if err != nil {
			return err
		}
		accumulator, selected := accumulators[fdcID]
		if !selected {
			return nil
		}
		nutrientID := record[2]
		if !isSupportedNutrient(nutrientID) {
			return nil
		}
		if strings.TrimSpace(record[3]) == "" {
			return nil
		}
		amount, err := parseFinite(record[3], "food_nutrient.amount")
		if err != nil || amount < 0 {
			diagnostics.InvalidNutrientAmounts++
			return nil
		}
		target := accumulatorTarget(&accumulator, nutrientID)
		if target.set {
			diagnostics.DuplicateNutrientRows++
			if target.value != amount {
				return fmt.Errorf("food %d has conflicting duplicate nutrient %s values %v and %v", fdcID, nutrientID, target.value, amount)
			}
			return nil
		}
		target.value = amount
		target.set = true
		accumulators[fdcID] = accumulator
		return nil
	})
}

func isSupportedNutrient(id string) bool {
	switch id {
	case "1008", "2048", "2047", "1003", "1005", "1004":
		return true
	default:
		return false
	}
}

func accumulatorTarget(accumulator *nutrientAccumulator, id string) *candidateValue {
	switch id {
	case "1008":
		return &accumulator.energy
	case "2048":
		return &accumulator.energyAtwaterSpecific
	case "2047":
		return &accumulator.energyAtwaterGeneral
	case "1003":
		return &accumulator.protein
	case "1005":
		return &accumulator.carbohydrates
	case "1004":
		return &accumulator.fat
	default:
		panic("unsupported nutrient " + id)
	}
}

func (i Importer) loadCandidateDescriptions(ctx context.Context, groups map[string]*brandGroup) (map[int64]string, error) {
	candidateIDs := make(map[int64]struct{}, len(groups))
	for _, group := range groups {
		if !group.eligible {
			continue
		}
		for _, fdcID := range group.versionIDs() {
			candidateIDs[fdcID] = struct{}{}
		}
	}
	descriptions := make(map[int64]string, len(candidateIDs))
	scanner, err := openCSV(i.DatasetDir, "food.csv", foodHeader, i.Logger)
	if err != nil {
		return nil, err
	}
	if err := scanner.Scan(ctx, func(record []string) error {
		fdcID, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			return err
		}
		if _, wanted := candidateIDs[fdcID]; !wanted {
			return nil
		}
		description := strings.TrimSpace(record[2])
		if description == "" {
			return fmt.Errorf("selected branded food %d has a blank description", fdcID)
		}
		descriptions[fdcID] = strings.Clone(description)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(descriptions) != len(candidateIDs) {
		return nil, fmt.Errorf("resolved %d of %d selected branded descriptions", len(descriptions), len(candidateIDs))
	}
	return descriptions, nil
}

func (i Importer) stageBranded(
	ctx context.Context,
	stage appimport.Stage,
	foodIndex []foodIndexEntry,
	groups map[string]*brandGroup,
	descriptions map[int64]string,
	accumulators map[int64]nutrientAccumulator,
	diagnostics *Diagnostics,
) error {
	candidateGroups := make(map[int64]*brandGroup, len(descriptions))
	for _, group := range groups {
		if !group.eligible {
			continue
		}
		for _, fdcID := range group.versionIDs() {
			candidateGroups[fdcID] = group
		}
	}

	references := make([]appimport.Reference, 0, i.BatchSize)
	scanner, err := openCSV(i.DatasetDir, "branded_food.csv", brandedFoodHeader, i.Logger)
	if err != nil {
		return err
	}
	err = scanner.Scan(ctx, func(record []string) error {
		fdcID, err := parsePositiveID(record[0], "branded_food.fdc_id")
		if err != nil {
			return err
		}
		gtin := strings.TrimSpace(record[4])
		group := groups[gtin]
		if group == nil || !group.eligible {
			return nil
		}
		references = append(references, appimport.Reference{ImportKey: importKeyForGTIN(gtin), ExternalID: strconv.FormatInt(fdcID, 10)})
		diagnostics.HistoricalUSDAReferences++
		if len(references) == cap(references) {
			if err := stage.StageReferences(ctx, references); err != nil {
				return err
			}
			references = references[:0]
		}

		candidateGroup := candidateGroups[fdcID]
		if candidateGroup == nil {
			return nil
		}
		discontinued, err := parseDate(record[16], "branded_food.discontinued_date", true)
		if err != nil {
			return err
		}
		candidateGroup.candidates = append(candidateGroup.candidates, brandCandidate{
			fdcID:            fdcID,
			canonicalName:    descriptions[fdcID],
			brand:            selectBrand(record[2], record[1]),
			nutrition:        accumulators[fdcID].canonical(),
			portion:          brandedPortion(importKeyForGTIN(gtin), record[7], record[8], record[9], diagnostics),
			discontinuedDate: discontinued,
		})
		return nil
	})
	if err != nil {
		return err
	}
	if len(references) > 0 {
		if err := stage.StageReferences(ctx, references); err != nil {
			return err
		}
	}

	foods := make([]appimport.Food, 0, i.BatchSize)
	portions := make([]appimport.Portion, 0, i.BatchSize)
	cutoff, _ := parseDate(i.DatasetDate.Format("2006-01-02"), "dataset date", false)
	groupKeys := slices.Sorted(maps.Keys(groups))
	for _, gtin := range groupKeys {
		group := groups[gtin]
		if !group.eligible {
			continue
		}
		expected := len(group.versionIDs())
		if len(group.candidates) != expected {
			return fmt.Errorf("GTIN %q resolved %d of %d final candidate payloads", gtin, len(group.candidates), expected)
		}
		slices.SortFunc(group.candidates, func(left, right brandCandidate) int {
			return int(left.fdcID - right.fdcID)
		})
		selected := group.candidates[0]
		for _, candidate := range group.candidates[1:] {
			if !selected.samePayload(candidate) {
				return fmt.Errorf("GTIN %q has unresolved latest-version payloads for FDC IDs %d and %d", gtin, selected.fdcID, candidate.fdcID)
			}
		}
		if selected.discontinuedDate != 0 && selected.discontinuedDate <= cutoff {
			diagnostics.DiscontinuedBrandedFoods++
			continue
		}
		gtinCopy := gtin
		foods = append(foods, appimport.Food{
			ImportKey:     importKeyForGTIN(gtin),
			GTIN:          &gtinCopy,
			SelectedFDCID: strconv.FormatInt(selected.fdcID, 10),
			CanonicalName: selected.canonicalName,
			Brand:         selected.brand,
			Nutrition:     selected.nutrition,
		})
		if selected.portion != nil {
			portions = append(portions, *selected.portion)
		}
		diagnostics.SelectedBrandedFoods++
		if len(foods) == cap(foods) {
			if err := stage.StageFoods(ctx, foods); err != nil {
				return err
			}
			foods = foods[:0]
		}
		if len(portions) == cap(portions) {
			if err := stage.StagePortions(ctx, portions); err != nil {
				return err
			}
			portions = portions[:0]
		}
	}
	if len(foods) > 0 {
		if err := stage.StageFoods(ctx, foods); err != nil {
			return err
		}
	}
	if len(portions) > 0 {
		if err := stage.StagePortions(ctx, portions); err != nil {
			return err
		}
	}
	_ = foodIndex // retained in the signature to emphasize that staged references were validated against food.csv.
	return nil
}

func selectBrand(brandName, brandOwner string) *string {
	value := strings.TrimSpace(brandName)
	if value == "" {
		value = strings.TrimSpace(brandOwner)
	}
	if value == "" {
		return nil
	}
	value = strings.Clone(value)
	return &value
}

func brandedPortion(importKey, servingSize, unit, household string, diagnostics *Diagnostics) *appimport.Portion {
	if unit != "g" || strings.TrimSpace(servingSize) == "" {
		diagnostics.UnsupportedBrandedServings++
		return nil
	}
	grams, err := parseFinite(servingSize, "branded_food.serving_size")
	if err != nil || grams <= 0 {
		diagnostics.InvalidPortions++
		return nil
	}
	measure := strings.TrimSpace(household)
	if measure == "" {
		measure = "serving"
	}
	return &appimport.Portion{ImportKey: importKey, Amount: 1, Measure: measure, Grams: grams}
}

func (i Importer) stageGeneric(ctx context.Context, stage appimport.Stage, generic map[int64]*genericFood, diagnostics *Diagnostics) error {
	ids := slices.Sorted(maps.Keys(generic))
	foods := make([]appimport.Food, 0, i.BatchSize)
	references := make([]appimport.Reference, 0, i.BatchSize)
	for _, fdcID := range ids {
		food := generic[fdcID]
		key := importKeyForFDC(fdcID)
		externalID := strconv.FormatInt(fdcID, 10)
		foods = append(foods, appimport.Food{
			ImportKey:     key,
			SelectedFDCID: externalID,
			CanonicalName: food.description,
			Nutrition:     food.nutrition,
		})
		references = append(references, appimport.Reference{ImportKey: key, ExternalID: externalID})
		diagnostics.SelectedGenericFoods++
		diagnostics.HistoricalUSDAReferences++
		if len(foods) == cap(foods) {
			if err := stage.StageFoods(ctx, foods); err != nil {
				return err
			}
			if err := stage.StageReferences(ctx, references); err != nil {
				return err
			}
			foods = foods[:0]
			references = references[:0]
		}
	}
	if len(foods) > 0 {
		if err := stage.StageFoods(ctx, foods); err != nil {
			return err
		}
		if err := stage.StageReferences(ctx, references); err != nil {
			return err
		}
	}
	return nil
}

func (i Importer) stageGenericPortions(ctx context.Context, stage appimport.Stage, generic map[int64]*genericFood, units map[string]string, diagnostics *Diagnostics) error {
	portions := make([]appimport.Portion, 0, i.BatchSize)
	scanner, err := openCSV(i.DatasetDir, "food_portion.csv", foodPortionHeader, i.Logger)
	if err != nil {
		return err
	}
	err = scanner.Scan(ctx, func(record []string) error {
		diagnostics.PortionRows++
		if strings.TrimSpace(record[1]) == "" {
			diagnostics.OrphanPortions++
			return nil
		}
		fdcID, err := parsePositiveID(record[1], "food_portion.fdc_id")
		if err != nil {
			diagnostics.OrphanPortions++
			return nil
		}
		food := generic[fdcID]
		if food == nil {
			return nil
		}
		portion, ok := mapGenericPortion(importKeyForFDC(fdcID), food.dataType, record, units)
		if !ok {
			diagnostics.InvalidPortions++
			return nil
		}
		portions = append(portions, portion)
		if len(portions) == cap(portions) {
			if err := stage.StagePortions(ctx, portions); err != nil {
				return err
			}
			portions = portions[:0]
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(portions) > 0 {
		return stage.StagePortions(ctx, portions)
	}
	return nil
}

func mapGenericPortion(importKey string, dataType uint8, record []string, units map[string]string) (appimport.Portion, bool) {
	grams, err := parseFinite(record[7], "food_portion.gram_weight")
	if err != nil || grams <= 0 {
		return appimport.Portion{}, false
	}

	var amount float64
	var measure string
	switch dataType {
	case dataTypeFoundation:
		amount, err = parseFinite(record[3], "food_portion.amount")
		if err != nil || amount <= 0 {
			return appimport.Portion{}, false
		}
		unit := units[strings.TrimSpace(record[4])]
		if unit == "" || strings.TrimSpace(record[4]) == "9999" {
			return appimport.Portion{}, false
		}
		parts := []string{unit}
		if modifier := strings.TrimSpace(record[6]); modifier != "" {
			parts = append(parts, modifier)
		}
		if description := strings.TrimSpace(record[5]); description != "" {
			parts = append(parts, description)
		}
		measure = strings.Join(parts, ", ")
	case dataTypeSRLegacy:
		amount, err = parseFinite(record[3], "food_portion.amount")
		if err != nil || amount <= 0 {
			return appimport.Portion{}, false
		}
		measure = strings.TrimSpace(record[6])
	case dataTypeSurveyFNDDS:
		amount = 1
		// USDA documents FNDDS portion_description as the complete household description.
		// Keep phrases such as "1 cup" source-native and free-form; do not parse the embedded amount.
		measure = strings.TrimSpace(record[5])
	default:
		return appimport.Portion{}, false
	}
	if measure == "" {
		return appimport.Portion{}, false
	}
	return appimport.Portion{ImportKey: importKey, Amount: amount, Measure: measure, Grams: grams}, true
}
