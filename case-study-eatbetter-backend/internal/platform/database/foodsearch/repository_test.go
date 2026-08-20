package foodsearch

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

func TestRepositoryStagesFuzzyOnlyWhenNeededAndNeverForShortQueries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		primary     string
		wantQueries int
	}{
		{name: "short", primary: "zz", wantQueries: 3},
		{name: "fuzzy eligible", primary: "zzz", wantQueries: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := &recordingQueryer{}
			_, err := New(database).Search(context.Background(), app.Query{
				Primary: test.primary, Folded: test.primary, Limit: 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(database.statements) != test.wantQueries {
				t.Fatalf("query count = %d, want %d", len(database.statements), test.wantQueries)
			}
		})
	}
}

func TestRankingIsLexicographicAndDeterministic(t *testing.T) {
	t.Parallel()
	candidates := []app.FoodCandidate{
		{FoodID: 6, Match: app.MatchMetadata{Class: app.MatchFuzzy, Form: app.FormPrimary, Source: app.SourceLocalizedDisplay, Similarity: 1}},
		{FoodID: 5, Match: app.MatchMetadata{Class: app.MatchPrefix, Form: app.FormPrimary, Source: app.SourceLocalizedDisplay}},
		{FoodID: 4, Match: app.MatchMetadata{Class: app.MatchWord, Form: app.FormFolded, Source: app.SourceLocalizedDisplay}},
		{FoodID: 3, Match: app.MatchMetadata{Class: app.MatchWord, Form: app.FormPrimary, Source: app.SourceLocalizedDisplay}},
		{FoodID: 2, Match: app.MatchMetadata{Class: app.MatchExact, Form: app.FormPrimary, Source: app.SourceBrand}},
		{FoodID: 1, Match: app.MatchMetadata{Class: app.MatchExact, Form: app.FormPrimary, Source: app.SourceLocalizedDisplay}},
	}
	for index := 0; index < len(candidates)-1; index++ {
		if !stronger(candidates[index+1], candidates[index]) {
			t.Fatalf("candidate %+v should outrank %+v", candidates[index+1], candidates[index])
		}
	}
	left := app.FoodCandidate{FoodID: 10, Match: app.MatchMetadata{Class: app.MatchExact}}
	right := app.FoodCandidate{FoodID: 11, Match: app.MatchMetadata{Class: app.MatchExact}}
	if !stronger(left, right) {
		t.Fatal("food ID must be the stable final tie-breaker")
	}
}

func TestRepositoryIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `TRUNCATE foods RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset test database (are migrations through 000005 applied?): %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `TRUNCATE foods RESTART IDENTITY CASCADE`) })

	ids := seedSearchFoods(t, ctx, pool)
	service := app.NewService(New(pool))

	assertFirst := func(query, locale string, expectedID int64, expectedDisplay string) []app.FoodCandidate {
		t.Helper()
		got, err := service.Search(ctx, app.Request{Query: query, Locale: locale, Limit: 10, LimitSet: true})
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		if len(got) == 0 || got[0].FoodID != expectedID || got[0].DisplayName != expectedDisplay {
			t.Fatalf("Search(%q) first = %+v, want id=%d display=%q", query, got, expectedID, expectedDisplay)
		}
		return got
	}

	assertFirst("süt", "tr-TR", ids["milk"], "Tam yağlı süt")
	ascii := assertFirst("sut", "tr", ids["milk"], "Tam yağlı süt")
	if ascii[0].Match.Class != app.MatchWord || ascii[0].Match.Form != app.FormFolded {
		t.Fatalf("ASCII Turkish whole-word ranking = %+v", ascii)
	}
	assertWordBeforeSutter(t, assertFirst("süt", "tr", ids["milk"], "Tam yağlı süt"), ids)
	assertWordBeforeSutter(t, ascii, ids)
	assertFirst("inek sütü", "tr", ids["milk"], "Tam yağlı süt")
	riceMatches := assertFirst("rice", "tr", ids["rice"], "Pirinç")
	if len(riceMatches) < 2 || riceMatches[0].Match.Class != app.MatchExact || riceMatches[1].FoodID != ids["brown_rice"] || riceMatches[1].Match.Class != app.MatchWord {
		t.Fatalf("exact/whole-word ranking = %+v", riceMatches)
	}
	for _, candidate := range riceMatches {
		if candidate.FoodID == ids["price"] && candidate.Match.Class == app.MatchWord {
			t.Fatalf("unsafe substring was treated as a whole word: %+v", candidate)
		}
	}
	assertFirst("rice", "de-DE", ids["rice"], "Rice")
	assertFirst("acme", "en-US", ids["branded"], "Crunchy Bread")
	assertFirst("crunch", "en", ids["branded"], "Crunchy Bread")
	assertFirst("brocoli", "tr", ids["broccoli"], "Çiğ brokoli")
	assertFirst("brok", "en", ids["broccoli"], "Broccoli, raw")
	milkResults := assertFirst("milk", "en", ids["milk"], "Milk, whole")
	if len(milkResults) < 2 || milkResults[0].IsBranded {
		t.Fatalf("generic milk was not represented before branded catalog noise: %+v", milkResults)
	}
	assertFirst("Kroger milk", "en", ids["kroger_milk"], "MILK")
	assertFirst("milk Kroger", "en", ids["kroger_milk"], "MILK")
	assertFirst("meijer", "en", ids["meijer_product"], "YOGURT")
	assertFirst("Great Value milk", "en", ids["great_value_milk"], "MILK")
	assertFirst("apple pie", "en", ids["apple_pie"], "Apple pie")

	duplicate := assertFirst("rice", "tr", ids["rice"], "Pirinç")
	count := 0
	for _, candidate := range duplicate {
		if candidate.FoodID == ids["rice"] {
			count++
		}
	}
	if count != 1 || duplicate[0].Match.Source != app.SourceCanonicalName {
		t.Fatalf("duplicate collapse/strongest signal = %+v", duplicate)
	}

	staleMatches, err := service.Search(ctx, app.Request{Query: "bayat süt", Locale: "tr"})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range staleMatches {
		if candidate.FoodID == ids["stale"] {
			t.Fatalf("stale localization contributed retrieval: %+v", candidate)
		}
	}
	assertFirst("Updated canonical", "tr", ids["stale"], "Updated canonical")

	limited, err := service.Search(ctx, app.Request{Query: "milk", Locale: "tr", Limit: 1, LimitSet: true})
	if err != nil || len(limited) != 1 {
		t.Fatalf("limited search = %+v, %v", limited, err)
	}
	repeated, err := service.Search(ctx, app.Request{Query: "milk", Locale: "tr", Limit: 1, LimitSet: true})
	if err != nil || repeated[0].FoodID != limited[0].FoodID {
		t.Fatalf("ordering is not deterministic: %+v then %+v", limited, repeated)
	}

	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := service.Search(cancelled, app.Request{Query: "milk"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search error = %v, want context.Canceled", err)
	}
}

func seedSearchFoods(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]int64 {
	t.Helper()
	ids := map[string]int64{}
	insertFood := func(key, canonical string, brand *string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name, brand) VALUES ($1, $2) RETURNING id`, canonical, brand).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[key] = id
		return id
	}
	identify := func(foodID int64, value string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO food_identifiers (food_id, scheme, value) VALUES ($1, 'gtin_upc', $2)`, foodID, value); err != nil {
			t.Fatal(err)
		}
	}
	localize := func(foodID int64, display, source string, aliases ...string) {
		t.Helper()
		var localizationID int64
		if err := pool.QueryRow(ctx, `
            INSERT INTO food_localizations (food_id, locale, display_name, source_canonical_name, source_fingerprint)
            VALUES ($1, 'tr', $2, $3, 'sha256:' || repeat('0', 64)) RETURNING id`, foodID, display, source).Scan(&localizationID); err != nil {
			t.Fatal(err)
		}
		for _, alias := range aliases {
			if _, err := pool.Exec(ctx, `INSERT INTO food_localization_aliases (localization_id, alias) VALUES ($1, $2)`, localizationID, alias); err != nil {
				t.Fatal(err)
			}
		}
	}

	milk := insertFood("milk", "Milk, whole", nil)
	localize(milk, "Tam yağlı süt", "Milk, whole", "inek sütü")
	for index, name := range []string{"SUTTER HOME", "SUTTER BUTTES OLIVE OIL", "SUTTER HILL"} {
		insertFood("sutter_"+string(rune('a'+index)), name, nil)
	}
	broccoli := insertFood("broccoli", "Broccoli, raw", nil)
	localize(broccoli, "Çiğ brokoli", "Broccoli, raw", "brokoli")
	rice := insertFood("rice", "Rice", nil)
	insertFood("brown_rice", "Brown rice", nil)
	insertFood("price", "Price cereal", nil)
	localize(rice, "Pirinç", "Rice", "rice")
	if _, err := pool.Exec(ctx, `INSERT INTO food_aliases (food_id, alias) VALUES ($1, 'rice')`, rice); err != nil {
		t.Fatal(err)
	}
	brand := "Acme"
	insertFood("branded", "Crunchy Bread", &brand)
	stale := insertFood("stale", "Updated canonical", nil)
	localize(stale, "Bayat süt", "Old canonical", "eski süt")
	insertFood("milk_prefix", "Milk chocolate", nil)
	for index := 0; index < 12; index++ {
		brand := "Catalog Brand " + string(rune('A'+index))
		key := "catalog_milk_" + string(rune('a'+index))
		foodID := insertFood(key, "MILK", &brand)
		identify(foodID, "0000000001"+string(rune('A'+index)))
	}
	kroger := "Kroger"
	krogerMilk := insertFood("kroger_milk", "MILK", &kroger)
	identify(krogerMilk, "000000000200")
	meijer := "Meijer"
	meijerProduct := insertFood("meijer_product", "YOGURT", &meijer)
	identify(meijerProduct, "000000000201")
	greatValue := "Great Value"
	greatValueMilk := insertFood("great_value_milk", "MILK", &greatValue)
	identify(greatValueMilk, "000000000202")
	apple := "Apple"
	appleProduct := insertFood("apple_product", "ORANGE JUICE", &apple)
	identify(appleProduct, "000000000203")
	insertFood("apple_pie", "Apple pie", nil)
	return ids
}

func assertWordBeforeSutter(t *testing.T, candidates []app.FoodCandidate, ids map[string]int64) {
	t.Helper()
	if len(candidates) < 4 || candidates[0].FoodID != ids["milk"] || candidates[0].Match.Class != app.MatchWord {
		t.Fatalf("milk whole-word result did not lead SUTTER prefixes: %+v", candidates)
	}
	for _, key := range []string{"sutter_a", "sutter_b", "sutter_c"} {
		found := false
		for _, candidate := range candidates[1:] {
			if candidate.FoodID == ids[key] {
				found = true
				if candidate.Match.Class != app.MatchPrefix {
					t.Fatalf("SUTTER candidate class = %v, want prefix: %+v", candidate.Match.Class, candidate)
				}
			}
		}
		if !found {
			t.Fatalf("expected seeded %s candidate in bounded result: %+v", key, candidates)
		}
	}
}

type recordingQueryer struct {
	statements []string
}

func (q *recordingQueryer) Query(_ context.Context, statement string, _ ...any) (pgx.Rows, error) {
	q.statements = append(q.statements, statement)
	return &emptyRows{}, nil
}

func (q *recordingQueryer) QueryRow(_ context.Context, statement string, _ ...any) pgx.Row {
	q.statements = append(q.statements, statement)
	return noRowsRow{}
}

type noRowsRow struct{}

func (noRowsRow) Scan(...any) error { return pgx.ErrNoRows }

type emptyRows struct{}

func (*emptyRows) Close()                                       {}
func (*emptyRows) Err() error                                   { return nil }
func (*emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (*emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (*emptyRows) Next() bool                                   { return false }
func (*emptyRows) Scan(...any) error                            { return nil }
func (*emptyRows) Values() ([]any, error)                       { return nil, nil }
func (*emptyRows) RawValues() [][]byte                          { return nil }
func (*emptyRows) Conn() *pgx.Conn                              { return nil }

func TestSQLUsesParametersAndBoundsEveryStage(t *testing.T) {
	t.Parallel()
	for name, statement := range map[string]string{"exact": exactSQL, "word": wordSQL, "prefix": prefixSQL, "fuzzy": fuzzySQL} {
		if !strings.Contains(statement, "LIMIT $5") {
			t.Errorf("%s query is not SQL-bounded", name)
		}
		for _, parameter := range []string{"$1", "$2", "$3", "$4", "$5"} {
			if !strings.Contains(statement, parameter) {
				t.Errorf("%s query does not use parameter %s", name, parameter)
			}
		}
	}
}

func TestProductSQLIsBoundedAndParameterized(t *testing.T) {
	t.Parallel()
	for name, statement := range map[string]string{
		"resolve brand":  resolveBrandSQL,
		"brand product":  brandProductSQL,
		"brand only":     brandOnlySQL,
		"generic strong": genericStrongSQL,
	} {
		if !strings.Contains(statement, "LIMIT") {
			t.Errorf("%s query is not bounded", name)
		}
		if !strings.Contains(statement, "$1") {
			t.Errorf("%s query is not parameterized", name)
		}
	}
}

func TestOrdinaryCompositionDoesNotPromoteGenericFuzzyNoise(t *testing.T) {
	t.Parallel()
	ordered := []app.FoodCandidate{
		{FoodID: 1, IsBranded: true, Match: app.MatchMetadata{Class: app.MatchExact, Source: app.SourceCanonicalName}},
		{FoodID: 2, Match: app.MatchMetadata{Class: app.MatchFuzzy, Source: app.SourceCanonicalName, Similarity: 0.4}},
	}
	got := composeOrdinary(ordered, 2)
	if len(got) != 2 || got[0].FoodID != 1 {
		t.Fatalf("composition = %+v, want exact branded before generic fuzzy", got)
	}
}

func TestOrdinaryCompositionReservesCredibleGenericLane(t *testing.T) {
	t.Parallel()
	ordered := []app.FoodCandidate{
		{FoodID: 1, IsBranded: true, Match: app.MatchMetadata{Class: app.MatchExact, Source: app.SourceCanonicalName}},
		{FoodID: 2, Match: app.MatchMetadata{Class: app.MatchWord, Source: app.SourceCanonicalName}},
		{FoodID: 3, IsBranded: true, Match: app.MatchMetadata{Class: app.MatchExact, Source: app.SourceCanonicalName}},
	}
	got := composeOrdinary(ordered, 2)
	if len(got) != 2 || got[0].FoodID != 2 || got[1].FoodID != 1 {
		t.Fatalf("composition = %+v, want credible generic then strongest remaining", got)
	}
}

func TestRealCatalogQueryPlans(t *testing.T) {
	databaseURL := os.Getenv("REAL_CATALOG_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("REAL_CATALOG_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var catalogSize int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM foods`).Scan(&catalogSize); err != nil {
		t.Fatal(err)
	}
	if catalogSize < 400_000 {
		t.Skipf("catalog has only %d foods; real-catalog plan check requires at least 400,000", catalogSize)
	}

	tests := []struct {
		name, statement, primary, folded string
	}{
		{name: "exact", statement: exactSQL, primary: "milk", folded: "milk"},
		{name: "word_turkish_folded", statement: wordSQL, primary: "süt", folded: "sut"},
		{name: "prefix", statement: prefixSQL, primary: "brok", folded: "brok"},
		{name: "fuzzy", statement: fuzzySQL, primary: "brocoli", folded: "brocoli"},
		{name: "brand", statement: exactSQL, primary: "meijer", folded: "meijer"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+test.statement,
				test.primary, test.folded, "tr-TR", "tr", 40)
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				lines = append(lines, line)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(lines, "\n")
			executionTime := "execution time unavailable"
			phase5IndexNodes := 0
			sequentialScanNodes := 0
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "Execution Time:") {
					executionTime = trimmed
				}
				if strings.Contains(trimmed, "Index Scan") && strings.Contains(trimmed, "_search_") {
					phase5IndexNodes++
				}
				if strings.Contains(trimmed, "Seq Scan on ") {
					sequentialScanNodes++
				}
			}
			t.Logf("%s plan for %d foods: %s; Phase 5 index nodes=%d; sequential scan nodes=%d",
				test.name, catalogSize, executionTime, phase5IndexNodes, sequentialScanNodes)
			if strings.Contains(plan, "Seq Scan on foods ") {
				t.Fatalf("%s plan performs a full foods scan", test.name)
			}
			if !strings.Contains(plan, "_search_") {
				t.Fatalf("%s plan did not use a Phase 5 search index", test.name)
			}
		})
	}
}

func TestRealCatalogProductQueryPlans(t *testing.T) {
	databaseURL := os.Getenv("REAL_CATALOG_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("REAL_CATALOG_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tests := []struct {
		name      string
		statement string
		arguments []any
	}{
		{
			name: "resolve_brand_phrases", statement: resolveBrandSQL,
			arguments: []any{
				[]string{"kroger milk", "kroger", "milk"},
				[]string{"kroger milk", "kroger", "milk"},
				[]int32{0, 0, 1}, []int32{2, 1, 2}, []int32{2, 1, 1},
			},
		},
		{
			name: "brand_product", statement: brandProductSQL,
			arguments: []any{"milk", "milk", "en", "en", 40, "kroger", "kroger", true},
		},
		{
			name: "brand_only", statement: brandOnlySQL,
			arguments: []any{"meijer", "meijer", "en", "en", 40},
		},
		{
			name: "generic_strong", statement: genericStrongSQL,
			arguments: []any{"milk", "milk", "en", "en", 50},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+test.statement, test.arguments...)
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				lines = append(lines, line)
			}
			rows.Close()
			plan := strings.Join(lines, "\n")
			if strings.Contains(plan, "Seq Scan on foods ") {
				t.Fatalf("%s performs a full foods scan:\n%s", test.name, plan)
			}
			if !strings.Contains(plan, "_search_") {
				t.Fatalf("%s did not use a search index:\n%s", test.name, plan)
			}
			for _, line := range lines {
				if strings.HasPrefix(strings.TrimSpace(line), "Execution Time:") {
					t.Logf("%s", strings.TrimSpace(line))
				}
			}
		})
	}
}
