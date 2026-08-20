// Package foodlocale defines the narrow locale value shared by food-facing features.
package foodlocale

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Locale is a normalized optional BCP-47 language/region pair.
type Locale struct {
	Exact string
	Base  string
}

// Parse validates the locale syntax supported by the food API.
func Parse(value string) (Locale, error) {
	if value == "" {
		return Locale{}, nil
	}
	parts := strings.Split(value, "-")
	if len(parts) < 1 || len(parts) > 2 || !asciiLetters(parts[0]) || len(parts[0]) < 2 || len(parts[0]) > 3 {
		return Locale{}, fmt.Errorf("malformed locale")
	}
	base := strings.ToLower(parts[0])
	if len(parts) == 1 {
		return Locale{Exact: base, Base: base}, nil
	}
	region := parts[1]
	if !((len(region) == 2 && asciiLetters(region)) || (len(region) == 3 && asciiDigits(region))) {
		return Locale{}, fmt.Errorf("malformed locale")
	}
	if asciiLetters(region) {
		region = strings.ToUpper(region)
	}
	return Locale{Exact: base + "-" + region, Base: base}, nil
}

func asciiLetters(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, current := range value {
		if current < 'A' || current > 'Z' {
			if current < 'a' || current > 'z' {
				return false
			}
		}
	}
	return value != ""
}

func asciiDigits(value string) bool {
	for _, current := range value {
		if current < '0' || current > '9' {
			return false
		}
	}
	return value != ""
}
