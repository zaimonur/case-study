// Package foodlocalization defines deterministic offline localization records.
package foodlocalization

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const (
	LocaleTurkish = "tr"
	SourceUSDA    = "usda"
	DatasetDate   = "2026-04-30"
)

// Status describes whether a generic food can be published without guessing.
type Status string

const (
	StatusLocalized      Status = "localized"
	StatusReviewRequired Status = "review_required"
	StatusUntranslated   Status = "untranslated"
)

// Candidate is one imported generic source food eligible for localization analysis.
type Candidate struct {
	ExternalID    string
	DataType      string
	CanonicalName string
}

// Record is one deterministic JSONL food record. Field order is part of the artifact format.
type Record struct {
	Source            string   `json:"source"`
	ExternalID        string   `json:"external_id"`
	DataType          string   `json:"data_type"`
	Locale            string   `json:"locale"`
	CanonicalName     string   `json:"canonical_name"`
	SourceFingerprint string   `json:"source_fingerprint"`
	Status            Status   `json:"status"`
	DisplayName       *string  `json:"display_name"`
	Aliases           []string `json:"aliases"`
	MatchedRuleIDs    []string `json:"matched_rule_ids"`
	ReasonCodes       []string `json:"reason_codes"`
}

// Fingerprint returns the versioned SHA-256 fingerprint of an exact trimmed canonical name.
func Fingerprint(canonicalName string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(canonicalName)))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Validate enforces the fail-closed artifact contract.
func (r Record) Validate() error {
	if r.Source != SourceUSDA {
		return fmt.Errorf("unsupported localization source %q", r.Source)
	}
	if externalID, err := strconv.ParseInt(r.ExternalID, 10, 64); err != nil || externalID <= 0 || strconv.FormatInt(externalID, 10) != r.ExternalID {
		return fmt.Errorf("external ID must be a canonical positive integer")
	}
	if !SupportedGenericDataType(r.DataType) {
		return fmt.Errorf("unsupported generic data type %q", r.DataType)
	}
	if r.Locale != LocaleTurkish {
		return fmt.Errorf("unsupported locale %q", r.Locale)
	}
	if r.CanonicalName == "" || r.CanonicalName != strings.TrimSpace(r.CanonicalName) {
		return fmt.Errorf("canonical name must be non-blank and trimmed")
	}
	if !norm.NFC.IsNormalString(r.CanonicalName) {
		return fmt.Errorf("canonical name must be NFC normalized")
	}
	if r.SourceFingerprint != Fingerprint(r.CanonicalName) {
		return fmt.Errorf("source fingerprint does not match canonical name")
	}
	if !slices.IsSorted(r.Aliases) || !slices.IsSorted(r.MatchedRuleIDs) || !slices.IsSorted(r.ReasonCodes) {
		return fmt.Errorf("aliases, matched rule IDs, and reason codes must be sorted")
	}
	if hasDuplicate(r.Aliases) || hasDuplicate(r.MatchedRuleIDs) || hasDuplicate(r.ReasonCodes) {
		return fmt.Errorf("aliases, matched rule IDs, and reason codes must not contain duplicates")
	}
	switch r.Status {
	case StatusLocalized:
		if r.DisplayName == nil || strings.TrimSpace(*r.DisplayName) == "" {
			return fmt.Errorf("localized record requires a display name")
		}
		if *r.DisplayName != strings.TrimSpace(*r.DisplayName) || !norm.NFC.IsNormalString(*r.DisplayName) {
			return fmt.Errorf("display name must be trimmed and NFC normalized")
		}
		if len(r.MatchedRuleIDs) == 0 || len(r.ReasonCodes) != 0 {
			return fmt.Errorf("localized record requires rules and no rejection reasons")
		}
		for _, alias := range r.Aliases {
			if alias == "" || alias != strings.TrimSpace(alias) || alias == *r.DisplayName || !norm.NFC.IsNormalString(alias) {
				return fmt.Errorf("localized aliases must be distinct, trimmed, non-blank NFC strings")
			}
		}
	case StatusReviewRequired, StatusUntranslated:
		if r.DisplayName != nil || len(r.Aliases) != 0 || len(r.ReasonCodes) == 0 {
			return fmt.Errorf("non-localized record requires no display/aliases and at least one reason")
		}
	default:
		return fmt.Errorf("unsupported localization status %q", r.Status)
	}
	return nil
}

// SupportedGenericDataType reports whether a USDA record is in Phase 4.5 scope.
func SupportedGenericDataType(value string) bool {
	switch value {
	case "foundation_food", "survey_fndds_food", "sr_legacy_food":
		return true
	default:
		return false
	}
}

func hasDuplicate(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return true
		}
	}
	return false
}
