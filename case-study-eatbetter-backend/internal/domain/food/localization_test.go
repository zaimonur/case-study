package food

import "testing"

func TestNewLocalization(t *testing.T) {
	t.Parallel()
	localization, err := NewLocalization(1, " tr ", " Çiğ brokoli ", " Broccoli, raw ", "sha256:732802e6f507ca5cd70282b71c87937acd2a48dafc13b337e8594eae19dc62e0")
	if err != nil {
		t.Fatal(err)
	}
	if localization.Locale != "tr" || localization.DisplayName != "Çiğ brokoli" || localization.SourceCanonicalName != "Broccoli, raw" {
		t.Fatalf("localization = %+v", localization)
	}
	if _, err := NewLocalization(1, "tr", "Çiğ brokoli", "Broccoli, raw", "not-a-fingerprint"); err == nil {
		t.Fatal("invalid source fingerprint accepted")
	}
}

func TestNewLocalizationAlias(t *testing.T) {
	t.Parallel()
	alias, err := NewLocalizationAlias(1, " çiğ frambuaz ")
	if err != nil || alias.Alias != "çiğ frambuaz" {
		t.Fatalf("alias = %+v, error = %v", alias, err)
	}
}
