// Package tr contains the conservative, deterministic English-to-Turkish rules.
package tr

import (
	"regexp"
	"slices"
	"strings"

	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodlocalization"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

const RulesetVersion = "tr-usda-v1"

const (
	ruleExactFamily     = "tr.family.exact.v1"
	ruleFamilyQualifier = "tr.family.qualifier.v1"
	ruleExactModifier   = "tr.modifier.exact.v1"
	ruleCatalogOrder    = "tr.render.catalog-order.v1"
	ruleExplicitAlias   = "tr.alias.explicit.v1"
	reasonAmbiguous     = "ambiguous_source"
	reasonCompound      = "compound_description"
	reasonNumeric       = "numeric_or_grade_qualifier"
	reasonAnimal        = "unsupported_animal_or_cut"
	reasonUnknownFamily = "unknown_food_family"
	reasonUnknownClause = "unknown_clause"
)

type familyTranslation struct {
	display string
	aliases []string
}

// Translator never guesses: every comma-delimited source clause must have an exact entry.
type Translator struct{}

// Translate returns one complete artifact record for an eligible USDA generic food.
func (Translator) Translate(candidate app.Candidate) app.Record {
	canonicalName := strings.TrimSpace(candidate.CanonicalName)
	record := app.Record{
		Source:            app.SourceUSDA,
		ExternalID:        candidate.ExternalID,
		DataType:          candidate.DataType,
		Locale:            app.LocaleTurkish,
		CanonicalName:     canonicalName,
		SourceFingerprint: app.Fingerprint(canonicalName),
		Aliases:           []string{},
		MatchedRuleIDs:    []string{},
		ReasonCodes:       []string{},
	}

	if reasons := sourceRiskReasons(canonicalName); len(reasons) > 0 {
		record.Status = app.StatusReviewRequired
		record.ReasonCodes = reasons
		return record
	}

	clauses := splitClauses(canonicalName)
	if len(clauses) == 0 {
		record.Status = app.StatusUntranslated
		record.ReasonCodes = []string{reasonUnknownFamily}
		return record
	}
	family, knownFamily := safeFamilies[clauses[0]]
	if !knownFamily {
		if _, risky := animalFamilies[clauses[0]]; risky {
			record.Status = app.StatusReviewRequired
			record.ReasonCodes = []string{reasonAnimal}
			return record
		}
		if _, risky := compoundFamilies[clauses[0]]; risky {
			record.Status = app.StatusReviewRequired
			record.ReasonCodes = []string{reasonCompound}
			return record
		}
		record.Status = app.StatusUntranslated
		record.ReasonCodes = []string{reasonUnknownFamily}
		return record
	}

	family, modifierStart, familyQualified := qualifyFamily(clauses, family)
	translated := make([]string, 0, len(clauses)-modifierStart)
	for _, clause := range clauses[modifierStart:] {
		value, known := translateModifier(clauses[0], clause)
		if !known {
			record.Status = app.StatusUntranslated
			record.ReasonCodes = []string{reasonUnknownClause}
			return record
		}
		translated = append(translated, value)
	}

	display := render(family.display, translated)
	record.Status = app.StatusLocalized
	record.DisplayName = &display
	record.MatchedRuleIDs = []string{ruleCatalogOrder, ruleExactFamily}
	if familyQualified {
		record.MatchedRuleIDs = append(record.MatchedRuleIDs, ruleFamilyQualifier)
	}
	if len(translated) > 0 {
		record.MatchedRuleIDs = append(record.MatchedRuleIDs, ruleExactModifier)
	}
	for _, aliasFamily := range family.aliases {
		alias := render(aliasFamily, translated)
		if alias != display {
			record.Aliases = append(record.Aliases, alias)
		}
	}
	if len(record.Aliases) > 0 {
		record.MatchedRuleIDs = append(record.MatchedRuleIDs, ruleExplicitAlias)
	}
	slices.Sort(record.Aliases)
	record.Aliases = slices.Compact(record.Aliases)
	slices.Sort(record.MatchedRuleIDs)
	return record
}

func qualifyFamily(clauses []string, family familyTranslation) (familyTranslation, int, bool) {
	if len(clauses) < 2 {
		return family, 1, false
	}
	switch clauses[0] + "\x00" + clauses[1] {
	case "Egg\x00white":
		return familyTranslation{display: "yumurta akı"}, 2, true
	case "Egg\x00yolk":
		return familyTranslation{display: "yumurta sarısı"}, 2, true
	case "Egg\x00whole":
		return familyTranslation{display: "bütün yumurta"}, 2, true
	case "Onions\x00green":
		return familyTranslation{display: "taze soğan"}, 2, true
	case "Beans\x00green":
		return familyTranslation{display: "taze fasulye"}, 2, true
	default:
		return family, 1, false
	}
}

func translateModifier(family, clause string) (string, bool) {
	if clause == "raw" && grainFamilies[family] {
		return "pişmemiş", true
	}
	if clause == "roasted" && roastingFamilies[family] {
		return "kavrulmuş", true
	}
	value, known := safeModifiers[clause]
	return value, known
}

func render(family string, modifiers []string) string {
	var value string
	if len(modifiers) == 0 {
		value = family
	} else if len(modifiers) == 1 && simplePrefixModifiers[modifiers[0]] {
		value = modifiers[0] + " " + family
	} else {
		value = family + "; " + strings.Join(modifiers, ", ")
	}
	return norm.NFC.String(sentenceCase(value))
}

func sentenceCase(value string) string {
	if value == "" {
		return value
	}
	first, size := rune(value[0]), 1
	if first >= 0x80 {
		for _, character := range value {
			first = character
			size = len(string(character))
			break
		}
	}
	return cases.Upper(language.Turkish).String(string(first)) + value[size:]
}

func splitClauses(value string) []string {
	parts := strings.Split(value, ",")
	clauses := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			clauses = append(clauses, part)
		}
	}
	return clauses
}

var (
	ambiguousPattern = regexp.MustCompile(`(?i)(^|[ ,;(])(NFS|NS as to)([ ,;)]|$)|and/or|\bor\b`)
	numericPattern   = regexp.MustCompile(`[0-9%"]|\b(all grades|choice|select|prime|marble score)\b`)
)

func sourceRiskReasons(value string) []string {
	reasons := make([]string, 0, 3)
	if strings.Contains(value, ";") || strings.ContainsAny(value, "()/") || ambiguousPattern.MatchString(value) {
		reasons = append(reasons, reasonAmbiguous)
	}
	if numericPattern.MatchString(strings.ToLower(value)) {
		reasons = append(reasons, reasonNumeric)
	}
	slices.Sort(reasons)
	return slices.Compact(reasons)
}

var safeFamilies = map[string]familyTranslation{
	"Almonds":          {display: "badem"},
	"Apples":           {display: "elma"},
	"Apricots":         {display: "kayısı"},
	"Artichokes":       {display: "enginar"},
	"Asparagus":        {display: "kuşkonmaz"},
	"Avocados":         {display: "avokado"},
	"Bananas":          {display: "muz"},
	"Barley":           {display: "arpa"},
	"Beans":            {display: "fasulye"},
	"Beets":            {display: "pancar"},
	"Blackberries":     {display: "böğürtlen"},
	"Blueberries":      {display: "yaban mersini"},
	"Breadfruit":       {display: "ekmek ağacı meyvesi"},
	"Broccoli":         {display: "brokoli"},
	"Brussels sprouts": {display: "Brüksel lahanası"},
	"Buckwheat":        {display: "karabuğday"},
	"Bulgur":           {display: "bulgur"},
	"Cabbage":          {display: "lahana"},
	"Carrots":          {display: "havuç"},
	"Cashew nuts":      {display: "kaju"},
	"Cauliflower":      {display: "karnabahar"},
	"Celery":           {display: "kereviz"},
	"Chard":            {display: "pazı"},
	"Cherries":         {display: "kiraz"},
	"Chickpeas":        {display: "nohut"},
	"Coconut meat":     {display: "Hindistan cevizi eti"},
	"Corn":             {display: "mısır"},
	"Cranberries":      {display: "turna yemişi"},
	"Cucumbers":        {display: "salatalık"},
	"Dates":            {display: "hurma"},
	"Egg":              {display: "yumurta"},
	"Eggplant":         {display: "patlıcan"},
	"Figs":             {display: "incir"},
	"Garlic":           {display: "sarımsak"},
	"Grapefruit":       {display: "greyfurt"},
	"Grapes":           {display: "üzüm"},
	"Guavas":           {display: "guava"},
	"Hazelnuts":        {display: "fındık"},
	"Kale":             {display: "kale"},
	"Kiwifruit":        {display: "kivi"},
	"Leeks":            {display: "pırasa"},
	"Lemons":           {display: "limon"},
	"Lentils":          {display: "mercimek"},
	"Lettuce":          {display: "marul"},
	"Limes":            {display: "misket limonu"},
	"Mangoes":          {display: "mango"},
	"Melons":           {display: "kavun"},
	"Millet":           {display: "darı"},
	"Mushrooms":        {display: "mantar"},
	"Nectarines":       {display: "nektarin"},
	"Okra":             {display: "bamya"},
	"Olives":           {display: "zeytin"},
	"Onions":           {display: "soğan"},
	"Oranges":          {display: "portakal"},
	"Oats":             {display: "yulaf"},
	"Papayas":          {display: "papaya"},
	"Pasta":            {display: "makarna"},
	"Parsley":          {display: "maydanoz"},
	"Parsnips":         {display: "yaban havucu"},
	"Peaches":          {display: "şeftali"},
	"Peanuts":          {display: "yer fıstığı"},
	"Pears":            {display: "armut"},
	"Peas":             {display: "bezelye"},
	"Peppers":          {display: "biber"},
	"Persimmons":       {display: "Trabzon hurması"},
	"Pineapple":        {display: "ananas"},
	"Pistachio nuts":   {display: "Antep fıstığı"},
	"Plantains":        {display: "plantain muzu"},
	"Plums":            {display: "erik"},
	"Pomegranates":     {display: "nar"},
	"Potatoes":         {display: "patates"},
	"Pumpkin":          {display: "bal kabağı"},
	"Quinoa":           {display: "kinoa"},
	"Radishes":         {display: "turp"},
	"Raspberries":      {display: "ahududu", aliases: []string{"frambuaz"}},
	"Rice":             {display: "pirinç"},
	"Rutabagas":        {display: "İsveç şalgamı"},
	"Soybeans":         {display: "soya fasulyesi"},
	"Spinach":          {display: "ıspanak"},
	"Squash":           {display: "kabak"},
	"Strawberries":     {display: "çilek"},
	"Sweet potato":     {display: "tatlı patates"},
	"Tangerines":       {display: "mandalina"},
	"Tomatillos":       {display: "tomatillo"},
	"Tomatoes":         {display: "domates"},
	"Turnips":          {display: "şalgam"},
	"Walnuts":          {display: "ceviz"},
	"Watermelon":       {display: "karpuz"},
	"Yam":              {display: "yam"},
	"Yardlong beans":   {display: "uzun fasulye"},
	"Yogurt":           {display: "yoğurt"},
	"Zucchini":         {display: "sakız kabağı"},
}

var safeModifiers = map[string]string{
	"all-purpose":        "çok amaçlı",
	"baked":              "fırında pişmiş",
	"black":              "siyah",
	"boiled":             "haşlanmış",
	"brown":              "esmer",
	"canned":             "konserve",
	"chopped":            "doğranmış",
	"cooked":             "pişmiş",
	"crumbled":           "ufalanmış",
	"diced":              "küp doğranmış",
	"drained":            "süzülmüş",
	"dried":              "kurutulmuş",
	"dry":                "kuru",
	"enriched":           "zenginleştirilmiş",
	"fat free":           "yağsız",
	"fortified":          "besin öğeleriyle zenginleştirilmiş",
	"fresh":              "taze",
	"frozen":             "dondurulmuş",
	"green":              "yeşil",
	"instant":            "hazır",
	"light syrup pack":   "hafif şurupta",
	"long-grain":         "uzun taneli",
	"low fat":            "az yağlı",
	"lowfat":             "az yağlı",
	"mashed":             "ezilmiş",
	"mature seeds":       "olgun taneli",
	"medium-grain":       "orta taneli",
	"no salt added":      "tuz eklenmemiş",
	"nonfat":             "yağsız",
	"packed in water":    "suda paketlenmiş",
	"pasteurized":        "pastörize",
	"peeled":             "soyulmuş",
	"plain":              "sade",
	"prepared":           "hazırlanmış",
	"pureed":             "püre haline getirilmiş",
	"raw":                "çiğ",
	"ready-to-eat":       "tüketime hazır",
	"red":                "kırmızı",
	"reduced fat":        "yağı azaltılmış",
	"regular":            "normal",
	"refrigerated":       "soğutulmuş",
	"roasted":            "fırınlanmış",
	"salted":             "tuzlanmış",
	"seeded":             "çekirdekleri çıkarılmış",
	"seedless":           "çekirdeksiz",
	"short-grain":        "kısa taneli",
	"sliced":             "dilimlenmiş",
	"solids and liquids": "katı ve sıvı kısmı",
	"sprouted":           "filizlendirilmiş",
	"steamed":            "buharda pişmiş",
	"sweetened":          "tatlandırılmış",
	"toasted":            "kızartılmış",
	"unpeeled":           "soyulmamış",
	"unenriched":         "zenginleştirilmemiş",
	"unprepared":         "hazırlanmamış",
	"unsalted":           "tuzsuz",
	"unsweetened":        "şekersiz",
	"water pack":         "suda paketlenmiş",
	"white":              "beyaz",
	"whole":              "bütün",
	"with salt":          "tuz eklenmiş",
	"without salt":       "tuzsuz",
	"yellow":             "sarı",
}

var simplePrefixModifiers = map[string]bool{
	"çiğ": true, "pişmemiş": true, "pişmiş": true, "haşlanmış": true, "fırında pişmiş": true,
	"fırınlanmış": true, "buharda pişmiş": true, "dondurulmuş": true,
	"kurutulmuş": true, "konserve": true, "taze": true, "soyulmuş": true,
}

var grainFamilies = map[string]bool{
	"Barley": true, "Buckwheat": true, "Bulgur": true, "Millet": true,
	"Oats": true, "Pasta": true, "Quinoa": true, "Rice": true,
}

var roastingFamilies = map[string]bool{
	"Almonds": true, "Cashew nuts": true, "Hazelnuts": true, "Peanuts": true,
	"Pistachio nuts": true, "Soybeans": true, "Walnuts": true,
}

var animalFamilies = map[string]struct{}{
	"Beef": {}, "Bison": {}, "Chicken": {}, "Duck": {}, "Fish": {}, "Game meat": {},
	"Goat": {}, "Goose": {}, "Lamb": {}, "Pork": {}, "Rabbit": {}, "Turkey": {}, "Veal": {},
}

var compoundFamilies = map[string]struct{}{
	"Babyfood": {}, "Beverages": {}, "Candies": {}, "Cereals": {}, "Fast foods": {},
	"Pizza": {}, "Restaurant": {}, "Sandwich": {}, "Snacks": {}, "Soup": {},
}
