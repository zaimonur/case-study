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

const RulesetVersion = "tr-usda-v2"

const (
	ruleExactFamily     = "tr.family.exact.v1"
	ruleFamilyQualifier = "tr.family.qualifier.v1"
	ruleExactModifier   = "tr.modifier.exact.v1"
	ruleCatalogOrder    = "tr.render.turkish-noun-phrase.v2"
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
	if qualifiers, known := familyQualifiers[clauses[0]]; known {
		last := min(len(clauses), 4)
		for end := last; end > 1; end-- {
			key := strings.ToLower(strings.Join(clauses[1:end], "\x00"))
			if qualified, known := qualifiers[key]; known {
				return qualified, end, true
			}
		}
	}
	return family, 1, false
}

func translateModifier(family, clause string) (string, bool) {
	normalized := strings.ToLower(clause)
	if normalized == "raw" && grainFamilies[family] {
		return "pişmemiş", true
	}
	if normalized == "roasted" && roastingFamilies[family] {
		return "kavrulmuş", true
	}
	value, known := safeModifiers[normalized]
	return value, known
}

func render(family string, modifiers []string) string {
	value := family
	if len(modifiers) > 0 {
		value = strings.Join(modifiers, ", ") + " " + family
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
	numericPattern   = regexp.MustCompile(`[0-9%"]|\b(grade [a-z0-9]+|all grades|choice|select|prime|marble score)\b`)
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
	"Bread":            {display: "ekmek"},
	"Buckwheat":        {display: "karabuğday"},
	"Bulgur":           {display: "bulgur"},
	"Butter":           {display: "tereyağı"},
	"Cabbage":          {display: "lahana"},
	"Carrots":          {display: "havuç"},
	"Cashew nuts":      {display: "kaju"},
	"Cauliflower":      {display: "karnabahar"},
	"Celery":           {display: "kereviz"},
	"Chard":            {display: "pazı"},
	"Cherries":         {display: "kiraz"},
	"Chickpeas":        {display: "nohut"},
	"Cheese":           {display: "peynir"},
	"Coconut meat":     {display: "Hindistan cevizi eti"},
	"Corn":             {display: "mısır"},
	"Corn flour":       {display: "mısır unu"},
	"Cornmeal":         {display: "mısır unu"},
	"Collards":         {display: "kara lahana"},
	"Coffee":           {display: "kahve"},
	"Crackers":         {display: "kraker"},
	"Cowpeas":          {display: "börülce"},
	"Cream":            {display: "krema"},
	"Cranberries":      {display: "turna yemişi"},
	"Cucumbers":        {display: "salatalık"},
	"Dates":            {display: "hurma"},
	"Egg":              {display: "yumurta"},
	"Eggplant":         {display: "patlıcan"},
	"Figs":             {display: "incir"},
	"Flour":            {display: "un"},
	"Garlic":           {display: "sarımsak"},
	"Grapefruit":       {display: "greyfurt"},
	"Grapes":           {display: "üzüm"},
	"Green beans":      {display: "taze fasulye"},
	"Green peas":       {display: "yeşil bezelye"},
	"Guavas":           {display: "guava"},
	"Hazelnuts":        {display: "fındık"},
	"Kale":             {display: "kale"},
	"Kiwifruit":        {display: "kivi"},
	"Leeks":            {display: "pırasa"},
	"Lemons":           {display: "limon"},
	"Lentils":          {display: "mercimek"},
	"Lettuce":          {display: "marul"},
	"Lima beans":       {display: "lima fasulyesi"},
	"Limes":            {display: "misket limonu"},
	"Mangoes":          {display: "mango"},
	"Melons":           {display: "kavun"},
	"Millet":           {display: "darı"},
	"Milk":             {display: "süt"},
	"Mushrooms":        {display: "mantar"},
	"Mushroom":         {display: "mantar"},
	"Mung beans":       {display: "maş fasulyesi"},
	"Mustard greens":   {display: "hardal otu"},
	"Nectarines":       {display: "nektarin"},
	"Noodles":          {display: "erişte"},
	"Nuts":             {display: "kuruyemiş"},
	"Okra":             {display: "bamya"},
	"Olives":           {display: "zeytin"},
	"Onions":           {display: "soğan"},
	"Oranges":          {display: "portakal"},
	"Oats":             {display: "yulaf"},
	"Oat flour":        {display: "yulaf unu"},
	"Oatmeal":          {display: "yulaf ezmesi"},
	"Oil":              {display: "yağ"},
	"Olive oil":        {display: "zeytinyağı"},
	"Papayas":          {display: "papaya"},
	"Pasta":            {display: "makarna"},
	"Parsley":          {display: "maydanoz"},
	"Parsnips":         {display: "yaban havucu"},
	"Peaches":          {display: "şeftali"},
	"Peanuts":          {display: "yer fıstığı"},
	"Peanut butter":    {display: "yer fıstığı ezmesi"},
	"Pears":            {display: "armut"},
	"Peas":             {display: "bezelye"},
	"Peppers":          {display: "biber"},
	"Persimmons":       {display: "Trabzon hurması"},
	"Pineapple":        {display: "ananas"},
	"Pistachio nuts":   {display: "Antep fıstığı"},
	"Plantains":        {display: "plantain muzu"},
	"Plums":            {display: "erik"},
	"Pomegranates":     {display: "nar"},
	"Popcorn":          {display: "patlamış mısır"},
	"Potatoes":         {display: "patates"},
	"Potato":           {display: "patates"},
	"Pumpkin":          {display: "bal kabağı"},
	"Quinoa":           {display: "kinoa"},
	"Radishes":         {display: "turp"},
	"Raspberries":      {display: "ahududu", aliases: []string{"frambuaz"}},
	"Rice":             {display: "pirinç"},
	"Rice flour":       {display: "pirinç unu"},
	"Rutabagas":        {display: "İsveç şalgamı"},
	"Seaweed":          {display: "deniz yosunu"},
	"Seeds":            {display: "tohum"},
	"Soybeans":         {display: "soya fasulyesi"},
	"Soy flour":        {display: "soya unu"},
	"Soy milk":         {display: "soya sütü"},
	"Soymilk":          {display: "soya sütü"},
	"Sour cream":       {display: "ekşi krema"},
	"Spices":           {display: "baharat"},
	"Spinach":          {display: "ıspanak"},
	"Squash":           {display: "kabak"},
	"Strawberries":     {display: "çilek"},
	"Sweet potato":     {display: "tatlı patates"},
	"Tangerines":       {display: "mandalina"},
	"Tea":              {display: "çay"},
	"Tomatillos":       {display: "tomatillo"},
	"Tomatoes":         {display: "domates"},
	"Turnips":          {display: "şalgam"},
	"Turnip greens":    {display: "şalgam yaprağı"},
	"Vegetables":       {display: "sebze"},
	"Walnuts":          {display: "ceviz"},
	"Watermelon":       {display: "karpuz"},
	"Water":            {display: "su"},
	"Wheat":            {display: "buğday"},
	"Wheat flour":      {display: "buğday unu"},
	"Yam":              {display: "yam"},
	"Yardlong beans":   {display: "uzun fasulye"},
	"Yogurt":           {display: "yoğurt"},
	"Zucchini":         {display: "sakız kabağı"},
}

var familyQualifiers = map[string]map[string]familyTranslation{
	"Beans": {
		"green":  {display: "taze fasulye"},
		"kidney": {display: "kidney fasulyesi"},
		"navy":   {display: "navy fasulyesi"},
		"pinto":  {display: "pinto fasulyesi"},
		"snap":   {display: "taze fasulye"},
	},
	"Butter": {
		"light":        {display: "hafif tereyağı"},
		"salted":       {display: "tuzlu tereyağı"},
		"stick":        {display: "kalıp tereyağı"},
		"tub":          {display: "kutu tereyağı"},
		"whipped":      {display: "çırpılmış tereyağı"},
		"without salt": {display: "tuzsuz tereyağı"},
	},
	"Bread": {
		"barley":                      {display: "arpa ekmeği"},
		"black":                       {display: "siyah ekmek"},
		"cheese":                      {display: "peynirli ekmek"},
		"cinnamon":                    {display: "tarçınlı ekmek"},
		"cuban":                       {display: "Küba ekmeği"},
		"egg":                         {display: "yumurtalı ekmek"},
		"gluten free":                 {display: "glutensiz ekmek"},
		"gluten-free":                 {display: "glutensiz ekmek"},
		"italian":                     {display: "İtalyan ekmeği"},
		"multigrain":                  {display: "çok tahıllı ekmek"},
		"naan":                        {display: "naan ekmeği"},
		"oat bran":                    {display: "yulaf kepekli ekmek"},
		"oatmeal":                     {display: "yulaf ezmeli ekmek"},
		"onion":                       {display: "soğanlı ekmek"},
		"pita":                        {display: "pita ekmeği"},
		"potato":                      {display: "patatesli ekmek"},
		"pumpernickel":                {display: "pumpernickel ekmeği"},
		"raisin":                      {display: "kuru üzümlü ekmek"},
		"reduced-calorie\x00oat bran": {display: "düşük kalorili yulaf kepekli ekmek"},
		"reduced-calorie\x00oatmeal":  {display: "düşük kalorili yulaf ezmeli ekmek"},
		"reduced-calorie\x00rye":      {display: "düşük kalorili çavdar ekmeği"},
		"reduced-calorie\x00wheat":    {display: "düşük kalorili buğday ekmeği"},
		"reduced-calorie\x00white":    {display: "düşük kalorili beyaz ekmek"},
		"rice":                        {display: "pirinç ekmeği"},
		"rye":                         {display: "çavdar ekmeği"},
		"soy":                         {display: "soya ekmeği"},
		"sprouted wheat":              {display: "filizlendirilmiş buğday ekmeği"},
		"sweet potato":                {display: "tatlı patatesli ekmek"},
		"wheat":                       {display: "buğday ekmeği"},
		"white":                       {display: "beyaz ekmek"},
		"white wheat":                 {display: "beyaz buğday ekmeği"},
		"whole grain white":           {display: "tam tahıllı beyaz ekmek"},
		"whole wheat":                 {display: "tam buğday ekmeği"},
		"whole-wheat":                 {display: "tam buğday ekmeği"},
	},
	"Cheese": {
		"american":                        {display: "Amerikan peyniri"},
		"blue":                            {display: "mavi küflü peynir"},
		"brick":                           {display: "brick peyniri"},
		"brie":                            {display: "brie peyniri"},
		"camembert":                       {display: "camembert peyniri"},
		"cheddar":                         {display: "çedar peyniri"},
		"colby":                           {display: "colby peyniri"},
		"colby jack":                      {display: "colby jack peyniri"},
		"cottage":                         {display: "cottage peyniri"},
		"cream":                           {display: "krem peynir"},
		"edam":                            {display: "edam peyniri"},
		"feta":                            {display: "feta peyniri"},
		"fontina":                         {display: "fontina peyniri"},
		"goat":                            {display: "keçi peyniri"},
		"goat\x00hard type":               {display: "sert keçi peyniri"},
		"goat\x00semisoft type":           {display: "yarı yumuşak keçi peyniri"},
		"goat\x00soft type":               {display: "yumuşak keçi peyniri"},
		"gouda":                           {display: "gouda peyniri"},
		"gruyere":                         {display: "gravyer peyniri"},
		"limburger":                       {display: "limburger peyniri"},
		"mexican blend":                   {display: "Meksika tipi peynir karışımı"},
		"mexican\x00queso anejo":          {display: "queso añejo peyniri"},
		"mexican\x00queso asadero":        {display: "queso asadero peyniri"},
		"mexican\x00queso chihuahua":      {display: "queso chihuahua peyniri"},
		"mexican\x00queso cotija":         {display: "cotija peyniri"},
		"monterey":                        {display: "monterey peyniri"},
		"monterey jack":                   {display: "monterey jack peyniri"},
		"mozzarella":                      {display: "mozzarella peyniri"},
		"muenster":                        {display: "muenster peyniri"},
		"neufchatel":                      {display: "neufchâtel peyniri"},
		"paneer":                          {display: "paneer peyniri"},
		"parmesan":                        {display: "parmesan peyniri"},
		"pasteurized process\x00american": {display: "Amerikan eritme peyniri"},
		"pasteurized process\x00pimento":  {display: "pimentolu eritme peyniri"},
		"pasteurized process\x00swiss":    {display: "İsviçre tipi eritme peyniri"},
		"port de salut":                   {display: "port-du-salut peyniri"},
		"provolone":                       {display: "provolone peyniri"},
		"queso fresco":                    {display: "queso fresco peyniri"},
		"ricotta":                         {display: "ricotta peyniri"},
		"romano":                          {display: "romano peyniri"},
		"roquefort":                       {display: "rokfor peyniri"},
		"swiss":                           {display: "İsviçre peyniri"},
		"dry white\x00queso seco":         {display: "queso seco peyniri"},
		"fresh\x00queso fresco":           {display: "queso fresco peyniri"},
		"white\x00queso blanco":           {display: "queso blanco peyniri"},
	},
	"Coffee": {
		"brewed":          {display: "demlenmiş kahve"},
		"cafe con leche":  {display: "sütlü kahve"},
		"cafe mocha":      {display: "cafe mocha"},
		"cappuccino":      {display: "cappuccino"},
		"espresso":        {display: "espresso"},
		"iced cafe mocha": {display: "buzlu cafe mocha"},
		"iced latte":      {display: "buzlu latte"},
		"instant":         {display: "hazır kahve"},
		"latte":           {display: "latte"},
		"macchiato":       {display: "macchiato"},
		"mocha":           {display: "mocha kahve"},
		"turkish":         {display: "Türk kahvesi"},
	},
	"Crackers": {
		"breakfast biscuit":   {display: "kahvaltılık bisküvi"},
		"butter":              {display: "tereyağlı kraker"},
		"cheese":              {display: "peynirli kraker"},
		"flatbread":           {display: "ince ekmek krakeri"},
		"flavored":            {display: "aromalı kraker"},
		"gluten free":         {display: "glutensiz kraker"},
		"gluten-free":         {display: "glutensiz kraker"},
		"matzo":               {display: "matsa krakeri"},
		"melba toast":         {display: "Melba tostu"},
		"milk":                {display: "sütlü kraker"},
		"multigrain":          {display: "çok tahıllı kraker"},
		"oatmeal":             {display: "yulaf ezmeli kraker"},
		"oyster":              {display: "oyster krakeri"},
		"rice":                {display: "pirinç krakeri"},
		"saltine":             {display: "tuzlu kraker"},
		"saltines":            {display: "tuzlu kraker"},
		"sandwich":            {display: "sandviç kraker"},
		"standard snack-type": {display: "standart atıştırmalık kraker"},
		"water":               {display: "su krakeri"},
		"water biscuits":      {display: "su krakeri"},
		"wheat":               {display: "buğday krakeri"},
		"whole grain":         {display: "tam tahıllı kraker"},
		"whole-wheat":         {display: "tam buğday krakeri"},
		"woven wheat":         {display: "dokuma buğday krakeri"},
	},
	"Egg": {
		"white": {display: "yumurta akı"},
		"whole": {display: "tam yumurta"},
		"yolk":  {display: "yumurta sarısı"},
	},
	"Flour": {
		"almond":      {display: "badem unu"},
		"amaranth":    {display: "amarant unu"},
		"barley":      {display: "arpa unu"},
		"bread":       {display: "ekmeklik un"},
		"buckwheat":   {display: "karabuğday unu"},
		"cassava":     {display: "manyok unu"},
		"chestnut":    {display: "kestane unu"},
		"coconut":     {display: "Hindistan cevizi unu"},
		"corn":        {display: "mısır unu"},
		"oat":         {display: "yulaf unu"},
		"pastry":      {display: "pastalık un"},
		"potato":      {display: "patates unu"},
		"quinoa":      {display: "kinoa unu"},
		"rice":        {display: "pirinç unu"},
		"rye":         {display: "çavdar unu"},
		"semolina":    {display: "irmik unu"},
		"sorghum":     {display: "sorgum unu"},
		"soy":         {display: "soya unu"},
		"spelt":       {display: "kavuzlu buğday unu"},
		"wheat":       {display: "buğday unu"},
		"whole wheat": {display: "tam buğday unu"},
	},
	"Cream": {
		"fluid":         {display: "sıvı krema"},
		"half and half": {display: "yarı süt yarı krema"},
		"heavy":         {display: "yoğun krema"},
		"light":         {display: "hafif krema"},
		"sour":          {display: "ekşi krema"},
		"whipped":       {display: "çırpılmış krema"},
	},
	"Milk": {
		"buttermilk":   {display: "yayık altı sütü"},
		"chocolate":    {display: "çikolatalı süt"},
		"condensed":    {display: "yoğunlaştırılmış süt"},
		"dry":          {display: "süt tozu"},
		"evaporated":   {display: "buharlaştırılmış süt"},
		"goat":         {display: "keçi sütü"},
		"lactose free": {display: "laktozsuz süt"},
		"sheep":        {display: "koyun sütü"},
		"whole":        {display: "tam yağlı süt"},
	},
	"Mushroom": {
		"beech":       {display: "kayın mantarı"},
		"crimini":     {display: "crimini mantarı"},
		"enoki":       {display: "enoki mantarı"},
		"king oyster": {display: "kral istiridye mantarı"},
		"maitake":     {display: "maitake mantarı"},
		"oyster":      {display: "istiridye mantarı"},
		"pioppini":    {display: "pioppini mantarı"},
		"portabella":  {display: "portobello mantarı"},
		"white":       {display: "beyaz mantar"},
	},
	"Mushrooms": {
		"chanterelle": {display: "kantarel mantarı"},
		"morel":       {display: "kuzu göbeği mantarı"},
		"oyster":      {display: "istiridye mantarı"},
		"shiitake":    {display: "shiitake mantarı"},
		"white":       {display: "beyaz mantar"},
	},
	"Noodles": {
		"chow mein":   {display: "chow mein eriştesi"},
		"egg":         {display: "yumurtalı erişte"},
		"whole grain": {display: "tam tahıllı erişte"},
	},
	"Nuts": {
		"almond butter":  {display: "badem ezmesi"},
		"almonds":        {display: "badem"},
		"brazilnuts":     {display: "Brezilya cevizi"},
		"cashew butter":  {display: "kaju ezmesi"},
		"cashew nuts":    {display: "kaju"},
		"chestnuts":      {display: "kestane"},
		"coconut meat":   {display: "Hindistan cevizi eti"},
		"macadamia nuts": {display: "makademya cevizi"},
		"mixed nuts":     {display: "karışık kuruyemiş"},
		"pecans":         {display: "pekan cevizi"},
		"pine nuts":      {display: "çam fıstığı"},
		"pistachio nuts": {display: "Antep fıstığı"},
		"walnuts":        {display: "ceviz"},
	},
	"Oil": {
		"almond":         {display: "badem yağı"},
		"apricot kernel": {display: "kayısı çekirdeği yağı"},
		"avocado":        {display: "avokado yağı"},
		"canola":         {display: "kanola yağı"},
		"coconut":        {display: "Hindistan cevizi yağı"},
		"corn":           {display: "mısır yağı"},
		"flaxseed":       {display: "keten tohumu yağı"},
		"grapeseed":      {display: "üzüm çekirdeği yağı"},
		"hazelnut":       {display: "fındık yağı"},
		"mustard":        {display: "hardal yağı"},
		"palm":           {display: "palmiye yağı"},
		"rice bran":      {display: "pirinç kepeği yağı"},
		"vegetable":      {display: "bitkisel yağ"},
		"walnut":         {display: "ceviz yağı"},
		"wheat germ":     {display: "buğday ruşeymi yağı"},
	},
	"Peanut butter": {
		"chunk style":  {display: "parçacıklı yer fıstığı ezmesi"},
		"chunky":       {display: "parçacıklı yer fıstığı ezmesi"},
		"creamy":       {display: "pürüzsüz yer fıstığı ezmesi"},
		"smooth":       {display: "pürüzsüz yer fıstığı ezmesi"},
		"smooth style": {display: "pürüzsüz yer fıstığı ezmesi"},
	},
	"Oatmeal": {
		"instant":    {display: "hazır yulaf ezmesi"},
		"multigrain": {display: "çok tahıllı yulaf ezmesi"},
	},
	"Potato": {
		"baked":        {display: "fırında pişmiş patates"},
		"boiled":       {display: "haşlanmış patates"},
		"canned":       {display: "konserve patates"},
		"cooked":       {display: "pişmiş patates"},
		"french fries": {display: "patates kızartması"},
		"hash brown":   {display: "hash brown patates"},
		"home fries":   {display: "tavada kızartılmış patates"},
		"mashed":       {display: "patates püresi"},
		"roasted":      {display: "fırınlanmış patates"},
	},
	"Popcorn": {
		"air-popped":     {display: "havayla patlatılmış mısır"},
		"caramel coated": {display: "karamel kaplı patlamış mısır"},
		"microwave":      {display: "mikrodalga patlamış mısır"},
		"popped in oil":  {display: "yağda patlatılmış mısır"},
		"ready-to-eat":   {display: "tüketime hazır patlamış mısır"},
	},
	"Seeds": {
		"chia seeds":                      {display: "chia tohumu"},
		"flaxseed":                        {display: "keten tohumu"},
		"hemp seed":                       {display: "kenevir tohumu"},
		"lotus seeds":                     {display: "lotus tohumu"},
		"pumpkin and squash seed kernels": {display: "kabak çekirdeği içi"},
		"pumpkin and squash seeds":        {display: "kabak çekirdeği"},
		"sesame butter":                   {display: "susam ezmesi", aliases: []string{"tahin"}},
		"sesame seeds":                    {display: "susam tohumu"},
		"sunflower seed":                  {display: "ayçiçeği çekirdeği"},
		"sunflower seed kernels":          {display: "ayçiçeği çekirdeği içi"},
		"watermelon seed kernels":         {display: "karpuz çekirdeği içi"},
	},
	"Spices": {
		"allspice":        {display: "yenibahar"},
		"anise seed":      {display: "anason tohumu"},
		"basil":           {display: "fesleğen"},
		"bay leaf":        {display: "defne yaprağı"},
		"caraway seed":    {display: "karaman kimyonu tohumu"},
		"cardamom":        {display: "kakule"},
		"celery seed":     {display: "kereviz tohumu"},
		"chervil":         {display: "frenk maydanozu"},
		"chili powder":    {display: "acı biber tozu"},
		"cinnamon":        {display: "tarçın"},
		"cloves":          {display: "karanfil"},
		"coriander leaf":  {display: "kişniş yaprağı"},
		"coriander seed":  {display: "kişniş tohumu"},
		"cumin seed":      {display: "kimyon tohumu"},
		"curry powder":    {display: "köri tozu"},
		"dill seed":       {display: "dereotu tohumu"},
		"dill weed":       {display: "dereotu"},
		"fennel seed":     {display: "rezene tohumu"},
		"fenugreek seed":  {display: "çemen tohumu"},
		"garlic powder":   {display: "sarımsak tozu"},
		"ginger":          {display: "zencefil"},
		"mace":            {display: "muskat çiçeği"},
		"marjoram":        {display: "mercanköşk"},
		"mustard seed":    {display: "hardal tohumu"},
		"nutmeg":          {display: "muskat"},
		"onion powder":    {display: "soğan tozu"},
		"oregano":         {display: "oregano"},
		"paprika":         {display: "toz kırmızı biber"},
		"parsley":         {display: "maydanoz"},
		"pepper\x00black": {display: "karabiber"},
		"pepper\x00white": {display: "beyaz biber"},
		"poppy seed":      {display: "haşhaş tohumu"},
		"rosemary":        {display: "biberiye"},
		"saffron":         {display: "safran"},
		"sage":            {display: "adaçayı"},
		"savory":          {display: "sater otu"},
		"tarragon":        {display: "tarhun"},
		"thyme":           {display: "kekik"},
		"turmeric":        {display: "zerdeçal"},
	},
	"Onions": {
		"green": {display: "taze soğan"},
	},
	"Peppers": {
		"chili":     {display: "şili biberi"},
		"hot chile": {display: "acı şili biberi"},
		"hungarian": {display: "Macar biberi"},
		"jalapeno":  {display: "jalapeño biberi"},
		"pasilla":   {display: "pasilla biberi"},
		"sweet":     {display: "tatlı biber"},
	},
	"Squash": {
		"summer":              {display: "yaz kabağı"},
		"summer\x00zucchini":  {display: "sakız kabağı"},
		"winter":              {display: "kış kabağı"},
		"winter\x00acorn":     {display: "acorn kabağı"},
		"winter\x00butternut": {display: "butternut kabağı"},
		"winter\x00hubbard":   {display: "hubbard kabağı"},
	},
	"Tea": {
		"bubble":                   {display: "baloncuklu çay"},
		"ginger":                   {display: "zencefil çayı"},
		"hot\x00chamomile":         {display: "sıcak papatya çayı"},
		"hot\x00herbal":            {display: "sıcak bitki çayı"},
		"hot\x00hibiscus":          {display: "sıcak hibiskus çayı"},
		"hot\x00leaf\x00black":     {display: "sıcak siyah çay"},
		"hot\x00leaf\x00green":     {display: "sıcak yeşil çay"},
		"hot\x00leaf\x00oolong":    {display: "sıcak oolong çayı"},
		"hot\x00with milk":         {display: "sıcak sütlü çay"},
		"iced\x00bottled\x00black": {display: "şişelenmiş soğuk siyah çay"},
		"iced\x00bottled\x00green": {display: "şişelenmiş soğuk yeşil çay"},
		"iced\x00brewed\x00black":  {display: "demlenmiş soğuk siyah çay"},
		"iced\x00brewed\x00green":  {display: "demlenmiş soğuk yeşil çay"},
		"iced\x00instant\x00black": {display: "hazır soğuk siyah çay"},
		"iced\x00instant\x00green": {display: "hazır soğuk yeşil çay"},
		"kombucha":                 {display: "kombu çayı"},
	},
}

var safeModifiers = map[string]string{
	"all-purpose":              "çok amaçlı",
	"all types":                "tüm türler",
	"all varieties":            "tüm çeşitler",
	"as ingredient":            "malzeme olarak",
	"baked":                    "fırında pişmiş",
	"bleached":                 "ağartılmış",
	"blanched":                 "zarı çıkarılmış",
	"black":                    "siyah",
	"boiled":                   "haşlanmış",
	"boiled and steamed":       "haşlanmış ve buharda pişmiş",
	"bottled":                  "şişelenmiş",
	"brewed":                   "demlenmiş",
	"brown":                    "esmer",
	"canned":                   "konserve",
	"chopped":                  "doğranmış",
	"cooked":                   "pişmiş",
	"commercially prepared":    "ticari olarak hazırlanmış",
	"cold pressed":             "soğuk sıkım",
	"crumbled":                 "ufalanmış",
	"cultured":                 "kültürlenmiş",
	"decaffeinated":            "kafeinsiz",
	"diced":                    "küp doğranmış",
	"diet":                     "diyet",
	"dehydrated":               "kurutulmuş",
	"drained":                  "süzülmüş",
	"drained and rinsed":       "süzülmüş ve durulanmış",
	"drained solids":           "süzülmüş katı kısmı",
	"dried":                    "kurutulmuş",
	"dry":                      "kuru",
	"dry roasted":              "kuru kavrulmuş",
	"enriched":                 "zenginleştirilmiş",
	"fat free":                 "yağsız",
	"fat added":                "yağ eklenmiş",
	"fortified":                "besin öğeleriyle zenginleştirilmiş",
	"fresh":                    "taze",
	"from dry mix":             "kuru karışımdan hazırlanmış",
	"from fresh":               "taze üründen hazırlanmış",
	"from frozen":              "dondurulmuş üründen hazırlanmış",
	"flavored":                 "aromalı",
	"fried":                    "kızartılmış",
	"frozen":                   "dondurulmuş",
	"gluten-free":              "glutensiz",
	"green":                    "yeşil",
	"greek":                    "Yunan usulü",
	"ground":                   "öğütülmüş",
	"hot":                      "sıcak",
	"hulled":                   "kabuğu çıkarılmış",
	"iced":                     "soğuk",
	"immature seeds":           "taze taneli",
	"instant":                  "hazır",
	"juice pack":               "meyve suyunda paketlenmiş",
	"leaf":                     "yaprak çaydan hazırlanmış",
	"light":                    "hafif",
	"lightly salted":           "hafif tuzlanmış",
	"light syrup pack":         "hafif şurupta",
	"long-grain":               "uzun taneli",
	"low fat":                  "az yağlı",
	"low salt":                 "az tuzlu",
	"low moisture":             "düşük nemli",
	"low sodium":               "az sodyumlu",
	"lowfat":                   "az yağlı",
	"made with butter":         "tereyağıyla hazırlanmış",
	"made with margarine":      "margarinle hazırlanmış",
	"made with milk":           "sütle hazırlanmış",
	"made with non-dairy milk": "bitkisel sütle hazırlanmış",
	"made with oil":            "yağla hazırlanmış",
	"made with water":          "suyla hazırlanmış",
	"maple flavored":           "akçaağaç aromalı",
	"mashed":                   "ezilmiş",
	"mature seeds":             "olgun taneli",
	"medium-grain":             "orta taneli",
	"microwaved":               "mikrodalgada pişirilmiş",
	"no salt added":            "tuz eklenmemiş",
	"no added fat":             "yağ eklenmemiş",
	"nonfat":                   "yağsız",
	"nonfat milk":              "yağsız sütle hazırlanmış",
	"not reconstituted":        "suyla hazırlanmamış",
	"oil roasted":              "yağda kavrulmuş",
	"oil-roasted":              "yağda kavrulmuş",
	"packed in water":          "suda paketlenmiş",
	"part skim":                "yarım yağlı sütten yapılmış",
	"part skim milk":           "yarım yağlı sütten yapılmış",
	"part-skim":                "yarım yağlı sütten yapılmış",
	"pasteurized":              "pastörize",
	"peeled":                   "soyulmuş",
	"pickled":                  "salamura",
	"plain":                    "sade",
	"pre-sweetened":            "önceden tatlandırılmış",
	"pre-sweetened with low calorie sweetener": "düşük kalorili tatlandırıcıyla önceden tatlandırılmış",
	"pre-sweetened with sugar":                 "şekerle önceden tatlandırılmış",
	"prepared":                                 "hazırlanmış",
	"prepared from recipe":                     "tarife göre hazırlanmış",
	"pureed":                                   "püre haline getirilmiş",
	"puree":                                    "püre",
	"raw":                                      "çiğ",
	"ready-to-eat":                             "tüketime hazır",
	"ready-to-heat":                            "ısıtmaya hazır",
	"red":                                      "kırmızı",
	"reconstituted":                            "suyla hazırlanmış",
	"reduced fat":                              "yağı azaltılmış",
	"reduced-calorie":                          "düşük kalorili",
	"reduced sodium":                           "sodyumu azaltılmış",
	"reduced sugar":                            "şekeri azaltılmış",
	"ripe":                                     "olgun",
	"regular":                                  "normal",
	"refrigerated":                             "soğutulmuş",
	"roasted":                                  "fırınlanmış",
	"salt added in processing":                 "işleme sırasında tuz eklenmiş",
	"salt not added in processing":             "işleme sırasında tuz eklenmemiş",
	"salted":                                   "tuzlanmış",
	"sauteed":                                  "sotelenmiş",
	"sharp":                                    "keskin aromalı",
	"seeded":                                   "çekirdekleri çıkarılmış",
	"seedless":                                 "çekirdeksiz",
	"short-grain":                              "kısa taneli",
	"sliced":                                   "dilimlenmiş",
	"solid":                                    "blok",
	"solids and liquids":                       "katı ve sıvı kısmıyla",
	"sour":                                     "ekşi",
	"sprouted":                                 "filizlendirilmiş",
	"steamed":                                  "buharda pişmiş",
	"stir-fried":                               "yüksek ateşte karıştırılarak kızartılmış",
	"sweet":                                    "tatlı",
	"sweetened":                                "tatlandırılmış",
	"fruit flavored":                           "meyve aromalı",
	"sun-dried":                                "güneşte kurutulmuş",
	"toasted":                                  "kızartılmış",
	"uncooked":                                 "pişmemiş",
	"unbleached":                               "ağartılmamış",
	"unblanched":                               "zarı çıkarılmamış",
	"unpeeled":                                 "soyulmamış",
	"unenriched":                               "zenginleştirilmemiş",
	"unprepared":                               "hazırlanmamış",
	"unsalted":                                 "tuzsuz",
	"unsweetened":                              "şekersiz",
	"water pack":                               "suda paketlenmiş",
	"heavy syrup":                              "koyu şurupta",
	"heavy syrup pack":                         "koyu şurupta",
	"extra heavy syrup pack":                   "çok koyu şurupta",
	"light syrup":                              "hafif şurupta",
	"pitted":                                   "çekirdeği çıkarılmış",
	"white":                                    "beyaz",
	"whole":                                    "bütün",
	"whole grain":                              "tam tahıllı",
	"whole-grain":                              "tam tahıllı",
	"whole milk":                               "tam yağlı sütten yapılmış",
	"with added calcium":                       "kalsiyum eklenmiş",
	"with added salt":                          "tuz eklenmiş",
	"with added vitamin a":                     "A vitamini eklenmiş",
	"with added vitamin d":                     "D vitamini eklenmiş",
	"with added vitamin a and vitamin d":       "A ve D vitaminleri eklenmiş",
	"with milk":                                "sütlü",
	"with non-dairy milk":                      "bitkisel sütle hazırlanmış",
	"with peanuts":                             "yer fıstığı içeren",
	"with salt added":                          "tuz eklenmiş",
	"with salt":                                "tuz eklenmiş",
	"without salt added":                       "tuz eklenmemiş",
	"without added vitamin a and vitamin d":    "A ve D vitaminleri eklenmemiş",
	"without peanuts":                          "yer fıstığı içermeyen",
	"without salt":                             "tuzsuz",
	"without added sugar":                      "şeker eklenmemiş",
	"with raisins":                             "kuru üzümlü",
	"with seeds":                               "çekirdekli",
	"with skin":                                "kabuğuyla",
	"peel eaten":                               "kabuğuyla",
	"peel not eaten":                           "kabuğu yenmeden",
	"includes skin":                            "kabuğuyla",
	"without peel":                             "kabuksuz",
	"without skin":                             "kabuksuz",
	"fluid":                                    "sıvı",
	"hard":                                     "sert",
	"shredded":                                 "rendelenmiş",
	"yellow":                                   "sarı",
}

var grainFamilies = map[string]bool{
	"Barley": true, "Buckwheat": true, "Bulgur": true, "Millet": true,
	"Oats": true, "Pasta": true, "Quinoa": true, "Rice": true, "Wheat": true,
}

var roastingFamilies = map[string]bool{
	"Almonds": true, "Cashew nuts": true, "Hazelnuts": true, "Peanuts": true,
	"Nuts": true, "Pistachio nuts": true, "Seeds": true, "Soybeans": true, "Walnuts": true,
}

var animalFamilies = map[string]struct{}{
	"Beef": {}, "Bison": {}, "Chicken": {}, "Duck": {}, "Fish": {}, "Game meat": {},
	"Goat": {}, "Goose": {}, "Lamb": {}, "Pork": {}, "Rabbit": {}, "Turkey": {}, "Veal": {},
}

var compoundFamilies = map[string]struct{}{
	"Babyfood": {}, "Beverages": {}, "Candies": {}, "Cereals": {}, "Fast foods": {},
	"Pizza": {}, "Restaurant": {}, "Sandwich": {}, "Snacks": {}, "Soup": {},
}
