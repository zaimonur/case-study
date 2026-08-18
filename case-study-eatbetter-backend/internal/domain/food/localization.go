package food

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Localization is a locale-specific display name derived from an exact canonical name.
type Localization struct {
	ID                  int64
	FoodID              int64
	Locale              string
	DisplayName         string
	SourceCanonicalName string
	SourceFingerprint   string
}

// NewLocalization creates a current localization without changing canonical food data.
func NewLocalization(foodID int64, locale, displayName, sourceCanonicalName, sourceFingerprint string) (Localization, error) {
	locale = strings.TrimSpace(locale)
	displayName = strings.TrimSpace(displayName)
	sourceCanonicalName = strings.TrimSpace(sourceCanonicalName)
	sourceFingerprint = strings.TrimSpace(sourceFingerprint)
	if foodID <= 0 {
		return Localization{}, fmt.Errorf("food ID must be positive")
	}
	if locale == "" {
		return Localization{}, fmt.Errorf("locale must not be empty")
	}
	if displayName == "" {
		return Localization{}, fmt.Errorf("display name must not be empty")
	}
	if sourceCanonicalName == "" {
		return Localization{}, fmt.Errorf("source canonical name must not be empty")
	}
	if !validSourceFingerprint(sourceFingerprint) {
		return Localization{}, fmt.Errorf("source fingerprint must be a lowercase SHA-256 fingerprint")
	}
	digest := sha256.Sum256([]byte(sourceCanonicalName))
	if sourceFingerprint != "sha256:"+hex.EncodeToString(digest[:]) {
		return Localization{}, fmt.Errorf("source fingerprint must match the source canonical name")
	}
	return Localization{
		FoodID:              foodID,
		Locale:              locale,
		DisplayName:         displayName,
		SourceCanonicalName: sourceCanonicalName,
		SourceFingerprint:   sourceFingerprint,
	}, nil
}

// LocalizationAlias is a search-only synonym owned by one localization.
type LocalizationAlias struct {
	ID             int64
	LocalizationID int64
	Alias          string
}

// NewLocalizationAlias creates a non-blank search alias.
func NewLocalizationAlias(localizationID int64, alias string) (LocalizationAlias, error) {
	alias = strings.TrimSpace(alias)
	if localizationID <= 0 {
		return LocalizationAlias{}, fmt.Errorf("localization ID must be positive")
	}
	if alias == "" {
		return LocalizationAlias{}, fmt.Errorf("localization alias must not be empty")
	}
	return LocalizationAlias{LocalizationID: localizationID, Alias: alias}, nil
}

func validSourceFingerprint(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
