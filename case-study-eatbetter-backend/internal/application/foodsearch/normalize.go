package foodsearch

import (
	"strings"
	"unicode"

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
