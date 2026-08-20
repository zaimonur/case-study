package foodlocale

import "testing"

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  Locale
	}{
		{"", Locale{}},
		{"tr", Locale{Exact: "tr", Base: "tr"}},
		{"TR-tr", Locale{Exact: "tr-TR", Base: "tr"}},
		{"de-419", Locale{Exact: "de-419", Base: "de"}},
	}
	for _, test := range tests {
		got, err := Parse(test.input)
		if err != nil || got != test.want {
			t.Fatalf("Parse(%q) = %+v, %v; want %+v", test.input, got, err, test.want)
		}
	}
}

func TestParseRejectsMalformedValues(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"t", "toolong", "tr_TR", "tr-", "tr-t", "tr-1234", "tr-TR-extra", "tr-ÜÇ"} {
		if _, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) error = nil", input)
		}
	}
}
