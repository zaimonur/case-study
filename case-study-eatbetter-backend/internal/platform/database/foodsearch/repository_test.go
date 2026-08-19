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
		{name: "short", primary: "zz", wantQueries: 2},
		{name: "fuzzy eligible", primary: "zzz", wantQueries: 3},
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
		{FoodID: 5, Match: app.MatchMetadata{Class: app.MatchFuzzy, Form: app.FormPrimary, Source: app.SourceLocalizedDisplay, Similarity: 1}},
		{FoodID: 4, Match: app.MatchMetadata{Class: app.MatchPrefix, Form: app.FormPrimary, Source: app.SourceLocalizedDisplay}},
		{FoodID: 3, Match: app.MatchMetadata{Class: app.MatchExact, Form: app.FormFolded, Source: app.SourceLocalizedDisplay}},
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
	ascii := assertFirst("sut", "tr", ids["ascii"], "sut")
	if len(ascii) < 2 || ascii[1].FoodID != ids["milk"] || ascii[0].Match.Form != app.FormPrimary || ascii[1].Match.Form != app.FormFolded {
		t.Fatalf("ASCII Turkish ranking = %+v", ascii)
	}
	assertFirst("inek sütü", "tr", ids["milk"], "Tam yağlı süt")
	assertFirst("rice", "tr", ids["rice"], "Pirinç")
	assertFirst("rice", "de-DE", ids["rice"], "Rice")
	assertFirst("acme", "en-US", ids["branded"], "Crunchy Bread")
	assertFirst("crunch", "en", ids["branded"], "Crunchy Bread")
	assertFirst("brocoli", "tr", ids["broccoli"], "Çiğ brokoli")

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
	localize(milk, "Tam yağlı süt", "Milk, whole", "süt", "inek sütü")
	insertFood("ascii", "sut", nil)
	broccoli := insertFood("broccoli", "Broccoli, raw", nil)
	localize(broccoli, "Çiğ brokoli", "Broccoli, raw", "brokoli")
	rice := insertFood("rice", "Rice", nil)
	localize(rice, "Pirinç", "Rice", "rice")
	if _, err := pool.Exec(ctx, `INSERT INTO food_aliases (food_id, alias) VALUES ($1, 'rice')`, rice); err != nil {
		t.Fatal(err)
	}
	brand := "Acme"
	insertFood("branded", "Crunchy Bread", &brand)
	stale := insertFood("stale", "Updated canonical", nil)
	localize(stale, "Bayat süt", "Old canonical", "eski süt")
	insertFood("milk_prefix", "Milk chocolate", nil)
	return ids
}

type recordingQueryer struct {
	statements []string
}

func (q *recordingQueryer) Query(_ context.Context, statement string, _ ...any) (pgx.Rows, error) {
	q.statements = append(q.statements, statement)
	return &emptyRows{}, nil
}

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
	for name, statement := range map[string]string{"exact": exactSQL, "prefix": prefixSQL, "fuzzy": fuzzySQL} {
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
		{name: "prefix", statement: prefixSQL, primary: "brok", folded: "brok"},
		{name: "fuzzy", statement: fuzzySQL, primary: "brocoli", folded: "brocoli"},
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
			t.Logf("%s plan for %d foods:\n%s", test.name, catalogSize, plan)
			if strings.Contains(plan, "Seq Scan on foods ") {
				t.Fatalf("%s plan performs a full foods scan", test.name)
			}
			if !strings.Contains(plan, "_search_") {
				t.Fatalf("%s plan did not use a Phase 5 search index", test.name)
			}
		})
	}
}
