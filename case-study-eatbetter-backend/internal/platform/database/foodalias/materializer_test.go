package foodalias

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSelectTurkishChickenCandidatesIsDeterministicBoundedAndConservative(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{
		{FoodID: 90, CanonicalName: "Chicken, feet, boiled"},
		{FoodID: 14, CanonicalName: "Chicken, broilers or fryers, meat and skin, cooked, roasted"},
		{FoodID: 11, CanonicalName: "Chicken, broilers or fryers, meat only, cooked, roasted"},
		{FoodID: 91, CanonicalName: "Chicken, liver, all classes, raw"},
		{FoodID: 10, CanonicalName: "Chicken, broilers or fryers, meat only, raw"},
		{FoodID: 12, CanonicalName: "Chicken, broilers or fryers, meat only, cooked, stewed"},
		{FoodID: 92, CanonicalName: "Chicken, meatless"},
		{FoodID: 13, CanonicalName: "Chicken, broilers or fryers, meat and skin, raw"},
		{FoodID: 15, CanonicalName: "Chicken, broilers or fryers, meat and skin, cooked, stewed"},
		{FoodID: 93, CanonicalName: "Chicken, ground, raw"},
		{FoodID: 94, CanonicalName: "Chicken, broilers or fryers, meat only, raw", IsBranded: true},
		{FoodID: 10, CanonicalName: "Chicken, broilers or fryers, meat only, raw"},
	}
	wantIDs := []int64{10, 11, 12, 13, 14, 15}

	first := SelectTurkishChickenCandidates(candidates)
	second := SelectTurkishChickenCandidates(reverseCandidates(candidates))
	if got := candidateIDs(first); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("selected IDs = %v, want %v", got, wantIDs)
	}
	if got := candidateIDs(second); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("selection changed with input order: %v, want %v", got, wantIDs)
	}
	if len(first) > maxTurkishChickenAliases || len(first) == len(candidates) {
		t.Fatalf("selection is not bounded: selected=%d catalog=%d", len(first), len(candidates))
	}
	for _, candidate := range first {
		if candidate.IsBranded || candidate.CanonicalName == "Chicken, meatless" ||
			candidate.CanonicalName == "Chicken, feet, boiled" || candidate.CanonicalName == "Chicken, liver, all classes, raw" {
			t.Fatalf("unsafe or narrow candidate selected: %+v", candidate)
		}
	}
}

func TestSelectTurkishChickenCandidatesSuppressesSingleton(t *testing.T) {
	t.Parallel()
	selected := SelectTurkishChickenCandidates([]Candidate{{
		FoodID: 1, CanonicalName: "Chicken, broilers or fryers, meat only, raw",
	}})
	if len(selected) != 0 {
		t.Fatalf("singleton selection = %+v, want none", selected)
	}
}

func TestMaterializeTurkishChickenAliasesIsIdempotentAndOwnsOnlyGeneratedRows(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE foods RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset test database (are migrations through 000006 applied?): %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `TRUNCATE foods RESTART IDENTITY CASCADE`) })

	for name := range turkishChickenPriority {
		insertUSDAFood(t, ctx, pool, name, false)
	}
	brandedID := insertUSDAFood(t, ctx, pool, "Chicken, broilers or fryers, meat only, raw", true)
	meatlessID := insertUSDAFood(t, ctx, pool, "Chicken, meatless", false)
	narrowID := insertUSDAFood(t, ctx, pool, "Chicken, feet, boiled", false)
	if _, err := pool.Exec(ctx, `INSERT INTO food_aliases (food_id, alias, language_tag) VALUES ($1, 'kanat', 'tr')`, narrowID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO food_aliases (food_id, alias, language_tag, materializer_key)
		VALUES ($1, 'tavuk', 'tr', $2)
	`, narrowID, TurkishChickenMaterializer); err != nil {
		t.Fatal(err)
	}

	first, err := Materialize(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.SelectedIDs) != 6 || first.Inserted != 6 || first.Deleted != 1 {
		t.Fatalf("first materialization = %+v", first)
	}
	second, err := Materialize(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.SelectedIDs, first.SelectedIDs) || second.Inserted != 0 || second.Deleted != 0 {
		t.Fatalf("second materialization = %+v, first = %+v", second, first)
	}

	assertAliasCount(t, ctx, pool, `alias='tavuk' AND language_tag='tr'`, 6)
	assertAliasCount(t, ctx, pool, `alias='tavuk' AND language_tag IS NULL`, 0)
	assertAliasCount(t, ctx, pool, fmt.Sprintf(`food_id IN (%d, %d) AND alias='tavuk'`, brandedID, meatlessID), 0)

	staleID := first.SelectedIDs[0]
	if _, err := pool.Exec(ctx, `UPDATE foods SET canonical_name='Chicken, feet, boiled' WHERE id=$1`, staleID); err != nil {
		t.Fatal(err)
	}
	third, err := Materialize(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(third.SelectedIDs) != 5 || third.Deleted != 1 || third.Inserted != 0 {
		t.Fatalf("stale cleanup materialization = %+v", third)
	}
	assertAliasCount(t, ctx, pool, fmt.Sprintf(`food_id=%d AND alias='tavuk'`, staleID), 0)
	assertAliasCount(t, ctx, pool, fmt.Sprintf(`food_id=%d AND alias='kanat'`, narrowID), 1)
}

func reverseCandidates(values []Candidate) []Candidate {
	reversed := make([]Candidate, len(values))
	for index := range values {
		reversed[len(values)-1-index] = values[index]
	}
	return reversed
}

func candidateIDs(values []Candidate) []int64 {
	ids := make([]int64, len(values))
	for index := range values {
		ids[index] = values[index].FoodID
	}
	return ids
}

func insertUSDAFood(t *testing.T, ctx context.Context, pool *pgxpool.Pool, canonicalName string, branded bool) int64 {
	t.Helper()
	var foodID int64
	if err := pool.QueryRow(ctx, `INSERT INTO foods (canonical_name) VALUES ($1) RETURNING id`, canonicalName).Scan(&foodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO external_food_refs (food_id, source, external_id) VALUES ($1, 'usda', $2)`, foodID, fmt.Sprint(foodID)); err != nil {
		t.Fatal(err)
	}
	if branded {
		if _, err := pool.Exec(ctx, `INSERT INTO food_identifiers (food_id, scheme, value) VALUES ($1, 'gtin_upc', $2)`, foodID, fmt.Sprintf("%012d", foodID)); err != nil {
			t.Fatal(err)
		}
	}
	return foodID
}

func assertAliasCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, predicate string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM food_aliases WHERE `+predicate).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("alias count for %q = %d, want %d", predicate, got, want)
	}
}
