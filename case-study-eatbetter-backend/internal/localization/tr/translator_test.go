package tr

import (
	"testing"

	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocalization"
)

func TestTranslatorLocalizesOnlyFullyConsumedSafeNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		canonical string
		want      string
	}{
		{"simple raw produce", "Broccoli, raw", "Çiğ brokoli"},
		{"grain raw means unprepared", "Rice, white, long-grain, raw, unenriched", "Pirinç; beyaz, uzun taneli, pişmemiş, zenginleştirilmemiş"},
		{"egg white is identity", "Egg, white, raw, frozen, pasteurized", "Yumurta akı; çiğ, dondurulmuş, pastörize"},
		{"green onion is identity", "Onions, green, raw", "Çiğ taze soğan"},
		{"nuts are roasted", "Peanuts, roasted, salted", "Yer fıstığı; kavrulmuş, tuzlanmış"},
		{"seeded means removed", "Peppers, seeded, raw", "Biber; çekirdekleri çıkarılmış, çiğ"},
	}
	translator := Translator{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := translator.Translate(app.Candidate{ExternalID: "1", DataType: "foundation_food", CanonicalName: test.canonical})
			if record.Status != app.StatusLocalized || record.DisplayName == nil || *record.DisplayName != test.want {
				t.Fatalf("Translate(%q) = status %q display %v, want %q", test.canonical, record.Status, record.DisplayName, test.want)
			}
			if err := record.Validate(); err != nil {
				t.Fatalf("generated record invalid: %v", err)
			}
		})
	}
}

func TestTranslatorFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		canonical string
		status    app.Status
		reason    string
	}{
		{"Chicken, breast, skinless, raw", app.StatusReviewRequired, reasonAnimal},
		{"Rice, brown, NS as to fat", app.StatusReviewRequired, reasonAmbiguous},
		{"Rice, white, 2% added fat", app.StatusReviewRequired, reasonNumeric},
		{"Broccoli, mystery preparation", app.StatusUntranslated, reasonUnknownClause},
		{"Unmapped family, raw", app.StatusUntranslated, reasonUnknownFamily},
	}
	translator := Translator{}
	for _, test := range tests {
		record := translator.Translate(app.Candidate{ExternalID: "1", DataType: "sr_legacy_food", CanonicalName: test.canonical})
		if record.Status != test.status || !contains(record.ReasonCodes, test.reason) {
			t.Fatalf("Translate(%q) = status %q reasons %v", test.canonical, record.Status, record.ReasonCodes)
		}
		if record.DisplayName != nil || len(record.Aliases) != 0 {
			t.Fatalf("unsafe record produced Turkish output: %+v", record)
		}
		if err := record.Validate(); err != nil {
			t.Fatalf("rejection record invalid: %v", err)
		}
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
