package foodlocalization

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestArtifactRoundTripIsDeterministic(t *testing.T) {
	t.Parallel()
	display := "Çiğ brokoli"
	records := []Record{
		{
			Source: SourceUSDA, ExternalID: "1", DataType: "foundation_food", Locale: LocaleTurkish,
			CanonicalName: "Broccoli, raw", SourceFingerprint: Fingerprint("Broccoli, raw"), Status: StatusLocalized,
			DisplayName: &display, Aliases: []string{}, MatchedRuleIDs: []string{"rule"}, ReasonCodes: []string{},
		},
		{
			Source: SourceUSDA, ExternalID: "2", DataType: "survey_fndds_food", Locale: LocaleTurkish,
			CanonicalName: "Complex food", SourceFingerprint: Fingerprint("Complex food"), Status: StatusUntranslated,
			Aliases: []string{}, MatchedRuleIDs: []string{}, ReasonCodes: []string{"unknown_food_family"},
		},
	}
	directory := t.TempDir()
	first := filepath.Join(directory, "first.jsonl")
	second := filepath.Join(directory, "second.jsonl")
	firstHash, err := WriteJSONL(first, records)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := WriteJSONL(second, records)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("artifact hashes differ: %s != %s", firstHash, secondHash)
	}
	firstBytes, _ := os.ReadFile(first)
	secondBytes, _ := os.ReadFile(second)
	if !slices.Equal(firstBytes, secondBytes) {
		t.Fatal("identical records produced different bytes")
	}

	manifest := NewManifest("2026-04-30", "rules-v1", firstHash, testInputFiles(), records)
	manifestPath := filepath.Join(directory, "manifest.json")
	if err := WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	readManifest, err := ReadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var read []Record
	if err := ReadJSONL(first, readManifest, func(record Record) error {
		read = append(read, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(read) != len(records) || read[0].CanonicalName != records[0].CanonicalName {
		t.Fatalf("round trip records = %+v", read)
	}
}

func TestArtifactRejectsTamperedBytes(t *testing.T) {
	t.Parallel()
	record := Record{
		Source: SourceUSDA, ExternalID: "2", DataType: "sr_legacy_food", Locale: LocaleTurkish,
		CanonicalName: "Unknown food", SourceFingerprint: Fingerprint("Unknown food"), Status: StatusUntranslated,
		Aliases: []string{}, MatchedRuleIDs: []string{}, ReasonCodes: []string{"unknown_food_family"},
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "artifact.jsonl")
	hash, err := WriteJSONL(path, []Record{record})
	if err != nil {
		t.Fatal(err)
	}
	manifest := NewManifest("2026-04-30", "rules-v1", hash, testInputFiles(), []Record{record})
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(" \n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ReadJSONL(path, manifest, nil); err == nil {
		t.Fatal("tampered artifact accepted")
	}
}

func testInputFiles() []InputFile {
	return []InputFile{
		{Name: "food.csv", SHA256: "0000000000000000000000000000000000000000000000000000000000000000"},
		{Name: "food_nutrient.csv", SHA256: "1111111111111111111111111111111111111111111111111111111111111111"},
		{Name: "nutrient.csv", SHA256: "2222222222222222222222222222222222222222222222222222222222222222"},
	}
}
