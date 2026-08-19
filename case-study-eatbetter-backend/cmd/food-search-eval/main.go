// Command food-search-eval runs a small deterministic retrieval check against a populated catalog.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
	dbfoodsearch "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/platform/database/foodsearch"
)

type evaluationCase struct {
	Category string
	Query    string
	Locale   string
	Expected []string
	NoResult bool
}

type queryResult struct {
	Category     string   `json:"category"`
	Query        string   `json:"query"`
	Locale       string   `json:"locale,omitempty"`
	CandidateIDs []int64  `json:"candidate_ids"`
	TopDisplays  []string `json:"top_displays"`
	Top1Hit      *bool    `json:"top_1_hit,omitempty"`
	Top5Hit      *bool    `json:"top_5_hit,omitempty"`
	NoResultOK   *bool    `json:"no_result_correct,omitempty"`
	LatencyMS    float64  `json:"latency_ms"`
	Error        string   `json:"error,omitempty"`
}

type report struct {
	Queries             []queryResult `json:"queries"`
	ExpectedCases       int           `json:"expected_cases"`
	Top1Hits            int           `json:"top_1_hits"`
	Top5Hits            int           `json:"top_5_hits"`
	Top1HitRate         float64       `json:"top_1_hit_rate"`
	Top5Recall          float64       `json:"top_5_recall"`
	NoResultCases       int           `json:"no_result_cases"`
	CorrectNoResults    int           `json:"correct_no_results"`
	NoResultCorrectness float64       `json:"no_result_correctness"`
	LatencyP50MS        float64       `json:"latency_p50_ms"`
	LatencyP95MS        float64       `json:"latency_p95_ms"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "populated PostgreSQL URL (or DATABASE_URL)")
	iterations := flag.Int("iterations", 1, "measured repetitions per query; best warmed result is evaluated")
	summaryOnly := flag.Bool("summary-only", false, "omit per-query rows from JSON output")
	failuresOnly := flag.Bool("failures-only", false, "include only failed expected/no-result cases in per-query output")
	flag.Parse()
	if *databaseURL == "" {
		return fmt.Errorf("database URL is required")
	}
	if *iterations < 1 || *iterations > 20 {
		return fmt.Errorf("iterations must be between 1 and 20")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	service := app.NewService(dbfoodsearch.New(pool))

	cases := evaluationCases()
	result := report{Queries: make([]queryResult, 0, len(cases))}
	latencies := make([]float64, 0, len(cases)**iterations)
	for _, test := range cases {
		var candidates []app.FoodCandidate
		var searchErr error
		bestLatency := time.Duration(1<<63 - 1)
		for iteration := 0; iteration < *iterations; iteration++ {
			started := time.Now()
			current, currentErr := service.Search(ctx, app.Request{Query: test.Query, Locale: test.Locale, Limit: 5, LimitSet: true})
			elapsed := time.Since(started)
			latencies = append(latencies, float64(elapsed.Microseconds())/1000)
			if elapsed < bestLatency {
				bestLatency, candidates, searchErr = elapsed, current, currentErr
			}
		}
		entry := evaluate(test, candidates, searchErr, bestLatency)
		result.Queries = append(result.Queries, entry)
		if entry.Top1Hit != nil {
			result.ExpectedCases++
			if *entry.Top1Hit {
				result.Top1Hits++
			}
			if *entry.Top5Hit {
				result.Top5Hits++
			}
		}
		if entry.NoResultOK != nil {
			result.NoResultCases++
			if *entry.NoResultOK {
				result.CorrectNoResults++
			}
		}
	}
	if result.ExpectedCases > 0 {
		result.Top1HitRate = float64(result.Top1Hits) / float64(result.ExpectedCases)
		result.Top5Recall = float64(result.Top5Hits) / float64(result.ExpectedCases)
	}
	if result.NoResultCases > 0 {
		result.NoResultCorrectness = float64(result.CorrectNoResults) / float64(result.NoResultCases)
	}
	sort.Float64s(latencies)
	result.LatencyP50MS = percentile(latencies, 0.50)
	result.LatencyP95MS = percentile(latencies, 0.95)
	if *summaryOnly {
		result.Queries = []queryResult{}
	} else if *failuresOnly {
		failures := result.Queries[:0]
		for _, query := range result.Queries {
			if (query.Top5Hit != nil && !*query.Top5Hit) || (query.NoResultOK != nil && !*query.NoResultOK) || query.Error != "" {
				failures = append(failures, query)
			}
		}
		result.Queries = failures
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func evaluate(test evaluationCase, candidates []app.FoodCandidate, err error, latency time.Duration) queryResult {
	result := queryResult{Category: test.Category, Query: test.Query, Locale: test.Locale, LatencyMS: float64(latency.Microseconds()) / 1000}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for _, candidate := range candidates {
		result.CandidateIDs = append(result.CandidateIDs, candidate.FoodID)
		result.TopDisplays = append(result.TopDisplays, candidate.DisplayName)
	}
	if test.NoResult {
		correct := len(candidates) == 0
		result.NoResultOK = &correct
	}
	if len(test.Expected) > 0 {
		top1 := len(candidates) > 0 && candidateMatches(candidates[0], test.Expected)
		top5 := false
		for _, candidate := range candidates {
			if candidateMatches(candidate, test.Expected) {
				top5 = true
				break
			}
		}
		result.Top1Hit, result.Top5Hit = &top1, &top5
	}
	return result
}

func candidateMatches(candidate app.FoodCandidate, expected []string) bool {
	brand := ""
	if candidate.Brand != nil {
		brand = *candidate.Brand
	}
	haystack := " " + app.Normalize(strings.Join([]string{candidate.CanonicalName, candidate.DisplayName, brand}, " ")).Folded + " "
	for _, value := range expected {
		if strings.Contains(haystack, " "+app.Normalize(value).Folded+" ") {
			return true
		}
	}
	return false
}

func percentile(sorted []float64, fraction float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * fraction)
	return sorted[index]
}

func evaluationCases() []evaluationCase {
	return []evaluationCase{
		{Category: "turkish", Query: "yumurta", Locale: "tr-TR", Expected: []string{"egg"}},
		{Category: "turkish", Query: "süt", Locale: "tr-TR", Expected: []string{"milk", "süt"}},
		{Category: "turkish", Query: "ekmek", Locale: "tr-TR", Expected: []string{"bread", "ekmek"}},
		{Category: "turkish", Query: "pirinç", Locale: "tr-TR", Expected: []string{"rice"}},
		{Category: "turkish", Query: "peynir", Locale: "tr-TR", Expected: []string{"cheese", "peynir"}},
		{Category: "turkish", Query: "brokoli", Locale: "tr-TR", Expected: []string{"broccoli"}},
		{Category: "turkish", Query: "buğday", Locale: "tr", Expected: []string{"wheat", "buckwheat"}},
		{Category: "turkish", Query: "tavuk", Locale: "tr", Expected: []string{"chicken"}},
		{Category: "turkish", Query: "elma", Locale: "tr", Expected: []string{"apple"}},
		{Category: "turkish", Query: "yoğurt", Locale: "tr", Expected: []string{"yogurt"}},
		{Category: "ascii_turkish", Query: "sut", Locale: "tr-TR", Expected: []string{"milk", "süt"}},
		{Category: "ascii_turkish", Query: "cig", Locale: "tr-TR", Expected: []string{"raw", "çiğ"}},
		{Category: "ascii_turkish", Query: "bugday", Locale: "tr", Expected: []string{"wheat", "buckwheat"}},
		{Category: "ascii_turkish", Query: "pirinc", Locale: "tr", Expected: []string{"rice"}},
		{Category: "ascii_turkish", Query: "yogurt", Locale: "tr", Expected: []string{"yogurt"}},
		{Category: "english", Query: "milk", Locale: "tr-TR", Expected: []string{"milk"}},
		{Category: "english", Query: "egg", Locale: "tr-TR", Expected: []string{"egg"}},
		{Category: "english", Query: "broccoli", Locale: "tr-TR", Expected: []string{"broccoli"}},
		{Category: "english", Query: "rice", Locale: "tr-TR", Expected: []string{"rice"}},
		{Category: "english", Query: "bread", Locale: "tr-TR", Expected: []string{"bread"}},
		{Category: "english", Query: "cheese", Locale: "tr", Expected: []string{"cheese"}},
		{Category: "english", Query: "chicken", Locale: "tr", Expected: []string{"chicken"}},
		{Category: "english", Query: "apple", Locale: "tr", Expected: []string{"apple"}},
		{Category: "english", Query: "yogurt", Locale: "tr", Expected: []string{"yogurt"}},
		{Category: "english", Query: "wheat", Locale: "tr", Expected: []string{"wheat"}},
		{Category: "misspelling", Query: "brocoli", Locale: "tr", Expected: []string{"broccoli"}},
		{Category: "misspelling", Query: "millk", Locale: "en", Expected: []string{"milk"}},
		{Category: "misspelling", Query: "bred", Locale: "en", Expected: []string{"bread"}},
		{Category: "misspelling", Query: "cheze", Locale: "en", Expected: []string{"cheese"}},
		{Category: "misspelling", Query: "yougurt", Locale: "en", Expected: []string{"yogurt"}},
		{Category: "brand", Query: "meijer", Locale: "en", Expected: []string{"meijer"}},
		{Category: "brand", Query: "wegmans", Locale: "en", Expected: []string{"wegmans"}},
		{Category: "brand", Query: "great value", Locale: "en", Expected: []string{"great value"}},
		{Category: "brand", Query: "kroger", Locale: "en", Expected: []string{"kroger"}},
		{Category: "brand", Query: "food club", Locale: "en", Expected: []string{"food club"}},
		{Category: "no_result", Query: "zzzxqvplm", Locale: "tr", NoResult: true},
		{Category: "no_result", Query: "qwxzplkjh", Locale: "en", NoResult: true},
		{Category: "no_result", Query: "vbnmqwzx", Locale: "de-DE", NoResult: true},
		{Category: "ambiguous", Query: "organic", Locale: "en"},
		{Category: "ambiguous", Query: "chocolate", Locale: "en"},
		{Category: "ambiguous", Query: "fresh", Locale: "en"},
		{Category: "ambiguous", Query: "raw", Locale: "en"},
	}
}
