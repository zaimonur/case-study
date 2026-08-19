package foodlocalization

import "testing"

func TestRecordValidation(t *testing.T) {
	t.Parallel()
	display := "Çiğ brokoli"
	valid := Record{
		Source: SourceUSDA, ExternalID: "321900", DataType: "foundation_food", Locale: LocaleTurkish,
		CanonicalName: "Broccoli, raw", SourceFingerprint: Fingerprint("Broccoli, raw"),
		Status: StatusLocalized, DisplayName: &display, Aliases: []string{},
		MatchedRuleIDs: []string{"rule.one"}, ReasonCodes: []string{},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}

	invalid := valid
	invalid.SourceFingerprint = Fingerprint("other")
	if err := invalid.Validate(); err == nil {
		t.Fatal("mismatched source fingerprint accepted")
	}
	invalid = valid
	invalid.Status = StatusUntranslated
	invalid.DisplayName = nil
	invalid.MatchedRuleIDs = []string{}
	if err := invalid.Validate(); err == nil {
		t.Fatal("untranslated record without reason accepted")
	}
	invalid.ReasonCodes = []string{"unknown_clause"}
	if err := invalid.Validate(); err != nil {
		t.Fatalf("valid untranslated record rejected: %v", err)
	}
}

func TestFingerprintIsExactAndStable(t *testing.T) {
	t.Parallel()
	const expected = "sha256:732802e6f507ca5cd70282b71c87937acd2a48dafc13b337e8594eae19dc62e0"
	if got := Fingerprint("Broccoli, raw"); got != expected {
		t.Fatalf("Fingerprint() = %q, want %q", got, expected)
	}
	if Fingerprint(" Broccoli, raw ") != expected {
		t.Fatal("Fingerprint() did not apply the canonical trim contract")
	}
}
