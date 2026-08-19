package bulkcsv

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"

	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocalization"
)

// LocalizationCatalog is the exact generic food population selected by the Phase 3B rules.
type LocalizationCatalog struct {
	Candidates  []app.Candidate
	Diagnostics Diagnostics
}

// ReadLocalizationCatalog reconstructs imported generic eligibility without writing to PostgreSQL.
func ReadLocalizationCatalog(ctx context.Context, datasetDirectory string, logger *slog.Logger) (LocalizationCatalog, error) {
	if datasetDirectory == "" {
		return LocalizationCatalog{}, fmt.Errorf("dataset directory is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	reader := Importer{DatasetDir: datasetDirectory, Logger: logger}
	for _, file := range []struct {
		name   string
		header []string
	}{{"food.csv", foodHeader}, {"food_nutrient.csv", foodNutrientHeader}, {"nutrient.csv", nutrientHeader}} {
		scanner, err := openCSV(datasetDirectory, file.name, file.header, nil)
		if err != nil {
			return LocalizationCatalog{}, err
		}
		if err := scanner.file.Close(); err != nil {
			return LocalizationCatalog{}, fmt.Errorf("close %s after header validation: %w", file.name, err)
		}
	}
	if err := reader.validateNutrients(ctx); err != nil {
		return LocalizationCatalog{}, err
	}

	var diagnostics Diagnostics
	_, generic, err := reader.loadFoodIndex(ctx, &diagnostics)
	if err != nil {
		return LocalizationCatalog{}, err
	}
	accumulators := make(map[int64]nutrientAccumulator, len(generic))
	for fdcID := range generic {
		accumulators[fdcID] = nutrientAccumulator{}
	}
	if err := reader.loadNutrition(ctx, accumulators, &diagnostics); err != nil {
		return LocalizationCatalog{}, err
	}
	for fdcID, food := range generic {
		food.nutrition = accumulators[fdcID].canonical()
		if food.nutrition.KnownCount() == 0 {
			delete(generic, fdcID)
			diagnostics.FoodsWithoutNutrition++
		}
	}

	ids := slices.Sorted(maps.Keys(generic))
	candidates := make([]app.Candidate, 0, len(ids))
	for _, fdcID := range ids {
		food := generic[fdcID]
		dataType, err := localizationDataType(food.dataType)
		if err != nil {
			return LocalizationCatalog{}, err
		}
		candidates = append(candidates, app.Candidate{
			ExternalID: strconv.FormatInt(fdcID, 10), DataType: dataType, CanonicalName: food.description,
		})
	}
	diagnostics.SelectedGenericFoods = int64(len(candidates))
	return LocalizationCatalog{Candidates: candidates, Diagnostics: diagnostics}, nil
}

func localizationDataType(value uint8) (string, error) {
	switch value {
	case dataTypeFoundation:
		return "foundation_food", nil
	case dataTypeSurveyFNDDS:
		return "survey_fndds_food", nil
	case dataTypeSRLegacy:
		return "sr_legacy_food", nil
	default:
		return "", fmt.Errorf("unsupported localization data type %d", value)
	}
}
