package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

const maxDatasetBytes = 8 * 1024 * 1024

func loadDataset(path string) ([]evaluationCase, string, int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", 0, fmt.Errorf("stat dataset: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxDatasetBytes {
		return nil, "", 0, fmt.Errorf("dataset size or type is invalid")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, "", 0, fmt.Errorf("read dataset: %w", err)
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	var cases []evaluationCase
	turnCount := 0
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var current evaluationCase
		if err := decodeStrict(line, &current); err != nil {
			return nil, hash, 0, fmt.Errorf("dataset line %d: %w", lineNumber, err)
		}
		if err := validateRuntimeCase(current); err != nil {
			return nil, hash, 0, fmt.Errorf("dataset line %d: %w", lineNumber, err)
		}
		cases = append(cases, current)
		turnCount += len(current.Turns)
	}
	if err := scanner.Err(); err != nil {
		return nil, hash, 0, fmt.Errorf("scan dataset: %w", err)
	}
	if len(cases) == 0 {
		return nil, hash, 0, fmt.Errorf("dataset contains no cases")
	}
	return cases, hash, turnCount, nil
}

func validateRuntimeCase(current evaluationCase) error {
	if strings.TrimSpace(current.ID) == "" || strings.TrimSpace(current.Category) == "" ||
		strings.TrimSpace(current.Locale) == "" || len(current.Tags) == 0 ||
		strings.TrimSpace(current.Notes) == "" || len(current.Turns) == 0 {
		return fmt.Errorf("missing required case field")
	}
	for turnIndex, turn := range current.Turns {
		if strings.TrimSpace(turn.Message) == "" || turn.Expect == nil || turn.Expect.MustNotAutoResolve == nil || turn.Expect.Items == nil {
			return fmt.Errorf("turn %d is missing a required field", turnIndex)
		}
		expect := turn.Expect
		if !oneOf(expect.Purpose, "meal_logging", "nutrition_query", "unknown") ||
			!oneOf(expect.State, "ready", "clarification_required", "empty") ||
			!oneOf(expect.ClarificationKind, "none", "amount", "food_identity") {
			return fmt.Errorf("turn %d has an unsupported expectation value", turnIndex)
		}
		if expect.State == "clarification_required" {
			if expect.ActiveItemIndex == nil || *expect.ActiveItemIndex < 0 || *expect.ActiveItemIndex >= len(expect.Items) {
				return fmt.Errorf("turn %d has an invalid active item", turnIndex)
			}
		} else if expect.ActiveItemIndex != nil {
			return fmt.Errorf("turn %d has an unexpected active item", turnIndex)
		}
		for itemIndex, item := range expect.Items {
			if item.SourceOrder == nil || *item.SourceOrder != itemIndex {
				return fmt.Errorf("turn %d item %d has invalid source order", turnIndex, itemIndex)
			}
			if item.ExpectedFoodID != nil && *item.ExpectedFoodID <= 0 {
				return fmt.Errorf("turn %d item %d has invalid food id", turnIndex, itemIndex)
			}
			for _, id := range item.AllowedFoodIDs {
				if id <= 0 {
					return fmt.Errorf("turn %d item %d has invalid allowed food id", turnIndex, itemIndex)
				}
			}
			if item.ExpectedResolvedGrams != nil && (!finitePositive(*item.ExpectedResolvedGrams)) {
				return fmt.Errorf("turn %d item %d has invalid grams", turnIndex, itemIndex)
			}
		}
	}
	return nil
}

func decodeStrict(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func finitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}
