package main

import (
	"strings"
	"testing"

	app "github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/application/foodsearch"
)

func TestGenericTop1ProductPolicy(t *testing.T) {
	t.Parallel()
	test := evaluationCase{ProductPolicy: policyGenericTop1, ProductExpectedFood: []string{"milk"}}
	tests := []struct {
		name      string
		candidate app.FoodCandidate
		want      bool
	}{
		{name: "relevant generic", candidate: app.FoodCandidate{CanonicalName: "Milk, whole"}, want: true},
		{name: "unrelated generic", candidate: app.FoodCandidate{CanonicalName: "Orange juice"}},
		{name: "branded food", candidate: app.FoodCandidate{CanonicalName: "MILK", IsBranded: true}},
		{name: "food term only in brand", candidate: app.FoodCandidate{CanonicalName: "Orange Juice", Brand: stringPointer("Milk Brand")}},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			if got := productPolicyMatches(test, []app.FoodCandidate{current.candidate}); got != current.want {
				t.Fatalf("productPolicyMatches() = %v, want %v", got, current.want)
			}
		})
	}
}

func TestBrandProductTop1RequiresBrandAndFoodOnSameCandidate(t *testing.T) {
	t.Parallel()
	test := evaluationCase{
		ProductPolicy: policyBrandTop1, ProductExpectedBrand: []string{"Kroger"}, ProductExpectedFood: []string{"milk"},
	}
	tests := []struct {
		name      string
		candidate app.FoodCandidate
		want      bool
	}{
		{name: "matching brand and food", candidate: app.FoodCandidate{CanonicalName: "MILK", Brand: stringPointer("Kroger"), IsBranded: true}, want: true},
		{name: "matching brand wrong food", candidate: app.FoodCandidate{CanonicalName: "YOGURT", Brand: stringPointer("Kroger"), IsBranded: true}},
		{name: "wrong brand matching food", candidate: app.FoodCandidate{CanonicalName: "MILK", Brand: stringPointer("Meijer"), IsBranded: true}},
		{name: "brand term only in food", candidate: app.FoodCandidate{CanonicalName: "Kroger Style Milk", Brand: stringPointer("Acme"), IsBranded: true}},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			if got := productPolicyMatches(test, []app.FoodCandidate{current.candidate}); got != current.want {
				t.Fatalf("productPolicyMatches() = %v, want %v", got, current.want)
			}
		})
	}
}

func TestBrandOnlyTop1ProductPolicy(t *testing.T) {
	t.Parallel()
	test := evaluationCase{ProductPolicy: policyBrandOnlyTop1, ProductExpectedBrand: []string{"Meijer"}}
	if !productPolicyMatches(test, []app.FoodCandidate{{CanonicalName: "YOGURT", Brand: stringPointer("Meijer"), IsBranded: true}}) {
		t.Fatal("Meijer brand-only candidate did not pass")
	}
	if productPolicyMatches(test, []app.FoodCandidate{{CanonicalName: "YOGURT", Brand: stringPointer("Kroger"), IsBranded: true}}) {
		t.Fatal("Kroger candidate passed Meijer brand-only expectation")
	}
}

func TestProductPolicyConfigurationFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []evaluationCase{
		{Query: "milk", ProductPolicy: policyGenericTop1},
		{Query: "Kroger milk", ProductPolicy: policyBrandTop1, ProductExpectedBrand: []string{"Kroger"}},
		{Query: "Kroger milk", ProductPolicy: policyBrandTop1, ProductExpectedFood: []string{"milk"}},
		{Query: "meijer", ProductPolicy: policyBrandOnlyTop1},
	}
	for _, current := range tests {
		if err := validateEvaluationCases([]evaluationCase{current}); err == nil {
			t.Errorf("validateEvaluationCases(%+v) error = nil", current)
		}
		if productPolicyMatches(current, []app.FoodCandidate{{CanonicalName: "MILK", Brand: stringPointer("Kroger")}}) {
			t.Errorf("misconfigured policy %+v passed", current)
		}
	}
}

func TestHistoricalLexicalMatchingRemainsORBasedAndMayInspectBrand(t *testing.T) {
	t.Parallel()
	candidate := app.FoodCandidate{CanonicalName: "Orange juice", Brand: stringPointer("Kroger")}
	if !candidateMatches(candidate, []string{"milk", "kroger"}) {
		t.Fatal("historical OR-based lexical expectation changed")
	}
}

func TestConfiguredEvaluationCasesAreValid(t *testing.T) {
	t.Parallel()
	if err := validateEvaluationCases(evaluationCases()); err != nil {
		t.Fatal(err)
	}
}

func TestProductPolicyEvaluationReportsConfigurationFailureAsFalse(t *testing.T) {
	t.Parallel()
	result := evaluate(evaluationCase{ProductPolicy: policyBrandTop1}, []app.FoodCandidate{{CanonicalName: "MILK", Brand: stringPointer("Kroger")}}, nil, 0)
	if result.ProductPolicyOK == nil || *result.ProductPolicyOK {
		t.Fatalf("ProductPolicyOK = %v, want false", result.ProductPolicyOK)
	}
}

func stringPointer(value string) *string {
	value = strings.Clone(value)
	return &value
}
