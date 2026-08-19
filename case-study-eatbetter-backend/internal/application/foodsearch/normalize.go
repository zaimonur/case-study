package foodsearch

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Forms contains the precision-preserving and tolerant forms of one query.
type Forms struct {
	Primary string
	Folded  string
}

// Normalize creates deterministic NFC, Turkish-aware search forms.
func Normalize(value string) Forms {
	primary := collapseSeparators(strings.ToLowerSpecial(unicode.TurkishCase, norm.NFC.String(value)))
	primary = norm.NFC.String(primary)
	return Forms{Primary: primary, Folded: foldTurkish(primary)}
}

func collapseSeparators(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	separatorPending := false
	for _, current := range value {
		if unicode.IsSpace(current) || unicode.IsPunct(current) {
			if result.Len() > 0 {
				separatorPending = true
			}
			continue
		}
		if separatorPending {
			result.WriteByte(' ')
			separatorPending = false
		}
		result.WriteRune(current)
	}
	return result.String()
}

func foldTurkish(value string) string {
	return strings.Map(func(current rune) rune {
		switch current {
		case 'ç':
			return 'c'
		case 'ğ':
			return 'g'
		case 'ı':
			return 'i'
		case 'ö':
			return 'o'
		case 'ş':
			return 's'
		case 'ü':
			return 'u'
		default:
			return current
		}
	}, value)
}

type locale struct {
	exact string
	base  string
}

func parseLocale(value string) (locale, error) {
	if value == "" {
		return locale{}, nil
	}
	parts := strings.Split(value, "-")
	if len(parts) < 1 || len(parts) > 2 || !asciiLetters(parts[0]) || len(parts[0]) < 2 || len(parts[0]) > 3 {
		return locale{}, fmt.Errorf("malformed locale")
	}
	base := strings.ToLower(parts[0])
	if len(parts) == 1 {
		return locale{exact: base, base: base}, nil
	}
	region := parts[1]
	if !((len(region) == 2 && asciiLetters(region)) || (len(region) == 3 && asciiDigits(region))) {
		return locale{}, fmt.Errorf("malformed locale")
	}
	if asciiLetters(region) {
		region = strings.ToUpper(region)
	}
	return locale{exact: base + "-" + region, base: base}, nil
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
