package foodlocalization

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const ArtifactFormatVersion = "eatbetter-food-localizations-v1"

// NamedCount is a deterministically ordered coverage counter.
type NamedCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// InputFile records the exact USDA source bytes used for generation.
type InputFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// DataTypeStatusCount records one status within one USDA generic data type.
type DataTypeStatusCount struct {
	DataType string `json:"data_type"`
	Status   string `json:"status"`
	Count    int64  `json:"count"`
}

// Manifest audits one complete deterministic localization artifact.
type Manifest struct {
	FormatVersion    string                `json:"format_version"`
	DatasetDate      string                `json:"dataset_date"`
	Locale           string                `json:"locale"`
	RulesetVersion   string                `json:"ruleset_version"`
	ArtifactSHA256   string                `json:"artifact_sha256"`
	Eligible         int64                 `json:"eligible"`
	Localized        int64                 `json:"localized"`
	ReviewRequired   int64                 `json:"review_required"`
	Untranslated     int64                 `json:"untranslated"`
	CoveragePercent  float64               `json:"coverage_percent"`
	InputFiles       []InputFile           `json:"input_files"`
	DataTypeCounts   []NamedCount          `json:"data_type_counts"`
	StatusCounts     []NamedCount          `json:"status_counts"`
	StatusByDataType []DataTypeStatusCount `json:"status_by_data_type"`
	RuleCounts       []NamedCount          `json:"rule_counts"`
	ReasonCounts     []NamedCount          `json:"reason_counts"`
	LocalizedAliases int64                 `json:"localized_aliases"`
}

// NewManifest calculates stable aggregate counts for a complete record set.
func NewManifest(datasetDate, rulesetVersion, artifactSHA256 string, inputFiles []InputFile, records []Record) Manifest {
	dataTypes := make(map[string]int64)
	statuses := make(map[string]int64)
	statusByDataType := make(map[string]int64)
	rules := make(map[string]int64)
	reasons := make(map[string]int64)
	var localized, reviewRequired, untranslated, aliases int64
	for _, record := range records {
		dataTypes[record.DataType]++
		statuses[string(record.Status)]++
		statusByDataType[record.DataType+"\x00"+string(record.Status)]++
		switch record.Status {
		case StatusLocalized:
			localized++
			aliases += int64(len(record.Aliases))
		case StatusReviewRequired:
			reviewRequired++
		case StatusUntranslated:
			untranslated++
		}
		for _, rule := range record.MatchedRuleIDs {
			rules[rule]++
		}
		for _, reason := range record.ReasonCodes {
			reasons[reason]++
		}
	}
	eligible := int64(len(records))
	coverage := float64(0)
	if eligible > 0 {
		coverage = float64(localized) * 100 / float64(eligible)
	}
	slices.SortFunc(inputFiles, func(left, right InputFile) int { return cmpString(left.Name, right.Name) })
	return Manifest{
		FormatVersion: ArtifactFormatVersion, DatasetDate: datasetDate, Locale: LocaleTurkish,
		RulesetVersion: rulesetVersion, ArtifactSHA256: artifactSHA256,
		Eligible: eligible, Localized: localized, ReviewRequired: reviewRequired,
		Untranslated: untranslated, CoveragePercent: coverage, InputFiles: inputFiles,
		DataTypeCounts: sortedCounts(dataTypes), StatusCounts: sortedCounts(statuses),
		StatusByDataType: sortedDataTypeStatusCounts(statusByDataType),
		RuleCounts:       sortedCounts(rules), ReasonCounts: sortedCounts(reasons), LocalizedAliases: aliases,
	}
}

// WriteJSONL writes validated records atomically and returns the file SHA-256.
func WriteJSONL(path string, records []Record) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create artifact directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".localizations-*.jsonl")
	if err != nil {
		return "", fmt.Errorf("create temporary artifact: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return "", fmt.Errorf("set artifact permissions: %w", err)
	}

	hash := sha256.New()
	writer := bufio.NewWriter(io.MultiWriter(temporary, hash))
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	var previousID int64
	for index, record := range records {
		if err := record.Validate(); err != nil {
			temporary.Close()
			return "", fmt.Errorf("validate artifact record %d: %w", index+1, err)
		}
		externalID, _ := strconv.ParseInt(record.ExternalID, 10, 64)
		if index > 0 && externalID <= previousID {
			temporary.Close()
			return "", fmt.Errorf("artifact records must be strictly sorted by numeric external ID")
		}
		previousID = externalID
		if err := encoder.Encode(record); err != nil {
			temporary.Close()
			return "", fmt.Errorf("encode artifact record %d: %w", index+1, err)
		}
	}
	if err := writer.Flush(); err != nil {
		temporary.Close()
		return "", fmt.Errorf("flush artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close artifact: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return "", fmt.Errorf("publish artifact: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// WriteManifest writes an indented, deterministic manifest atomically.
func WriteManifest(path string, manifest Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	contents = append(contents, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".localizations-manifest-*.json")
	if err != nil {
		return fmt.Errorf("create temporary manifest: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set manifest permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close manifest: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("publish manifest: %w", err)
	}
	return nil
}

// ReadManifest reads and validates the immutable artifact metadata.
func ReadManifest(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Manifest{}, fmt.Errorf("decode manifest: trailing JSON content")
	}
	if manifest.FormatVersion != ArtifactFormatVersion || manifest.DatasetDate != DatasetDate || manifest.Locale != LocaleTurkish || strings.TrimSpace(manifest.RulesetVersion) == "" {
		return Manifest{}, fmt.Errorf("unsupported or incomplete localization manifest")
	}
	if len(manifest.ArtifactSHA256) != 64 {
		return Manifest{}, fmt.Errorf("manifest artifact SHA-256 is invalid")
	}
	if _, err := hex.DecodeString(manifest.ArtifactSHA256); err != nil {
		return Manifest{}, fmt.Errorf("manifest artifact SHA-256 is invalid: %w", err)
	}
	if manifest.Eligible != manifest.Localized+manifest.ReviewRequired+manifest.Untranslated {
		return Manifest{}, fmt.Errorf("manifest status counts do not equal eligible count")
	}
	expectedCoverage := float64(0)
	if manifest.Eligible > 0 {
		expectedCoverage = float64(manifest.Localized) * 100 / float64(manifest.Eligible)
	}
	if math.Abs(manifest.CoveragePercent-expectedCoverage) > 1e-12 {
		return Manifest{}, fmt.Errorf("manifest coverage percentage is inconsistent")
	}
	expectedInputs := []string{"food.csv", "food_nutrient.csv", "nutrient.csv"}
	if len(manifest.InputFiles) != len(expectedInputs) {
		return Manifest{}, fmt.Errorf("manifest must identify the three USDA input files")
	}
	for index, input := range manifest.InputFiles {
		if input.Name != expectedInputs[index] || len(input.SHA256) != 64 || strings.ToLower(input.SHA256) != input.SHA256 {
			return Manifest{}, fmt.Errorf("manifest USDA input file metadata is invalid")
		}
		if _, err := hex.DecodeString(input.SHA256); err != nil {
			return Manifest{}, fmt.Errorf("manifest USDA input file SHA-256 is invalid: %w", err)
		}
	}
	for _, values := range [][]NamedCount{manifest.DataTypeCounts, manifest.StatusCounts, manifest.RuleCounts, manifest.ReasonCounts} {
		if !slices.IsSortedFunc(values, func(left, right NamedCount) int { return cmpString(left.Name, right.Name) }) {
			return Manifest{}, fmt.Errorf("manifest count arrays must be sorted")
		}
	}
	if !slices.IsSortedFunc(manifest.StatusByDataType, compareDataTypeStatusCount) {
		return Manifest{}, fmt.Errorf("manifest status-by-data-type counts must be sorted")
	}
	return manifest, nil
}

// ReadJSONL streams validated records and verifies the artifact byte hash and count.
func ReadJSONL(path string, manifest Manifest, visit func(Record) error) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(file, hash))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var count int64
	var previousID int64
	seen := make(map[string]struct{}, manifest.Eligible)
	dataTypes := make(map[string]int64)
	statuses := make(map[string]int64)
	statusByDataType := make(map[string]int64)
	rules := make(map[string]int64)
	reasons := make(map[string]int64)
	var localized, reviewRequired, untranslated, aliases int64
	for scanner.Scan() {
		count++
		var record Record
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return fmt.Errorf("decode artifact line %d: %w", count, err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return fmt.Errorf("decode artifact line %d: trailing JSON content", count)
		}
		if err := record.Validate(); err != nil {
			return fmt.Errorf("validate artifact line %d: %w", count, err)
		}
		externalID, _ := strconv.ParseInt(record.ExternalID, 10, 64)
		if count > 1 && externalID <= previousID {
			return fmt.Errorf("artifact records are not strictly sorted by numeric external ID at line %d", count)
		}
		previousID = externalID
		key := record.Source + "\x00" + record.ExternalID
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate artifact identity %s/%s", record.Source, record.ExternalID)
		}
		seen[key] = struct{}{}
		dataTypes[record.DataType]++
		statuses[string(record.Status)]++
		statusByDataType[record.DataType+"\x00"+string(record.Status)]++
		switch record.Status {
		case StatusLocalized:
			localized++
			aliases += int64(len(record.Aliases))
		case StatusReviewRequired:
			reviewRequired++
		case StatusUntranslated:
			untranslated++
		}
		for _, rule := range record.MatchedRuleIDs {
			rules[rule]++
		}
		for _, reason := range record.ReasonCodes {
			reasons[reason]++
		}
		if visit != nil {
			if err := visit(record); err != nil {
				return fmt.Errorf("process artifact line %d: %w", count, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan artifact: %w", err)
	}
	if count != manifest.Eligible {
		return fmt.Errorf("artifact has %d records, manifest requires %d", count, manifest.Eligible)
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if actualHash != manifest.ArtifactSHA256 {
		return fmt.Errorf("artifact SHA-256 %s does not match manifest %s", actualHash, manifest.ArtifactSHA256)
	}
	if localized != manifest.Localized || reviewRequired != manifest.ReviewRequired || untranslated != manifest.Untranslated || aliases != manifest.LocalizedAliases ||
		!reflect.DeepEqual(sortedCounts(dataTypes), manifest.DataTypeCounts) || !reflect.DeepEqual(sortedCounts(statuses), manifest.StatusCounts) ||
		!reflect.DeepEqual(sortedDataTypeStatusCounts(statusByDataType), manifest.StatusByDataType) ||
		!reflect.DeepEqual(sortedCounts(rules), manifest.RuleCounts) || !reflect.DeepEqual(sortedCounts(reasons), manifest.ReasonCounts) {
		return fmt.Errorf("artifact aggregate counts do not match manifest")
	}
	return nil
}

func sortedDataTypeStatusCounts(values map[string]int64) []DataTypeStatusCount {
	counts := make([]DataTypeStatusCount, 0, len(values))
	for key, count := range values {
		parts := strings.SplitN(key, "\x00", 2)
		counts = append(counts, DataTypeStatusCount{DataType: parts[0], Status: parts[1], Count: count})
	}
	slices.SortFunc(counts, compareDataTypeStatusCount)
	return counts
}

func compareDataTypeStatusCount(left, right DataTypeStatusCount) int {
	if result := cmpString(left.DataType, right.DataType); result != 0 {
		return result
	}
	return cmpString(left.Status, right.Status)
}

// HashFile returns a lowercase SHA-256 for one source file.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func sortedCounts(values map[string]int64) []NamedCount {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	counts := make([]NamedCount, 0, len(names))
	for _, name := range names {
		counts = append(counts, NamedCount{Name: name, Count: values[name]})
	}
	return counts
}

func cmpString(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
