package main

import (
	"os"
	"path/filepath"
	"testing"
)

const validDatasetLine = `{"id":"case","category":"unknown_non_food","tags":["test"],"locale":"tr-TR","turns":[{"message":"hava","expect":{"purpose":"unknown","state":"empty","clarification_kind":"none","must_not_auto_resolve":true,"items":[]}}],"notes":"test"}`

func TestStrictDatasetParsing(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "success", body: validDatasetLine + "\n"},
		{name: "malformed", body: `{"id":` + "\n", wantErr: true},
		{name: "trailing value", body: validDatasetLine + ` {}` + "\n", wantErr: true},
		{name: "unknown field", body: validDatasetLine[:len(validDatasetLine)-1] + `,"extra":true}` + "\n", wantErr: true},
		{name: "missing runtime field", body: `{"id":"case","category":"unknown_non_food","tags":["test"],"locale":"tr-TR","turns":[],"notes":"test"}` + "\n", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dataset.jsonl")
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			cases, hash, turns, err := loadDataset(path)
			if test.wantErr {
				if err == nil {
					t.Fatalf("loadDataset succeeded: %#v %q %d", cases, hash, turns)
				}
				return
			}
			if err != nil || len(cases) != 1 || turns != 1 || hash == "" {
				t.Fatalf("loadDataset = %#v %q %d %v", cases, hash, turns, err)
			}
		})
	}
}

func TestCLIValidationRejectsUnsafeInputs(t *testing.T) {
	for _, args := range [][]string{
		{"-base-url", "http://user:secret@localhost:8080"},
		{"-base-url", "http://localhost:8080/path"},
		{"-timeout", "-1s"},
		{"-case-delay", "-1s"},
		{"-retry-backoff", "-1s"},
		{"-max-retries", "11"},
		{"-model-label", "bad\nlabel"},
	} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("parseCLI accepted %#v", args)
		}
	}
}
