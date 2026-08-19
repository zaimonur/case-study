package bulkcsv

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestReadLocalizationCatalogUsesImporterEligibility(t *testing.T) {
	t.Parallel()
	catalog, err := ReadLocalizationCatalog(context.Background(), writeSyntheticDataset(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Candidates) != 3 {
		t.Fatalf("candidates = %d, want 3: %+v", len(catalog.Candidates), catalog.Candidates)
	}
	want := []struct{ id, dataType string }{{"200", "foundation_food"}, {"201", "survey_fndds_food"}, {"202", "sr_legacy_food"}}
	for index, expected := range want {
		if got := catalog.Candidates[index]; got.ExternalID != expected.id || got.DataType != expected.dataType {
			t.Fatalf("candidate %d = %+v, want ID %s type %s", index, got, expected.id, expected.dataType)
		}
	}
}
