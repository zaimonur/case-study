package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const frozenDatasetTestPath = "../../data/evaluation/mealai-chat-v1.jsonl"

func TestCompleteRunWithAccuracyFailuresExitsZeroAndPublishesAtomically(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writeChatResponse(t, writer, unknownChat())
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "baseline.json")
	var stdout, stderr bytes.Buffer
	exitCode := runCLI([]string{
		"-base-url", server.URL,
		"-dataset", frozenDatasetTestPath,
		"-timeout", "1s",
		"-case-delay", "0s",
		"-max-retries", "0",
		"-retry-backoff", "0s",
		"-output", output,
		"-model-label", "test-model",
	}, &stdout, &stderr)
	if exitCode != 0 || stderr.Len() != 0 || requests != 30 {
		t.Fatalf("exit/stderr/requests = %d / %q / %d", exitCode, stderr.String(), requests)
	}
	var got report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.RunStatus != "COMPLETE" || got.ProductFailureCases == 0 || got.ModelLabel == nil || *got.ModelLabel != "test-model" {
		t.Fatalf("report = %#v", got)
	}
	published, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(published, stdout.Bytes()) {
		t.Fatal("published baseline differs from stdout report")
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(output), ".meal-ai-eval-*.tmp")); len(matches) != 0 {
		t.Fatalf("temporary output remains: %#v", matches)
	}
}

func TestIncompleteRunExitsNonZeroAndDoesNotPublishOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeStatusResponse(t, writer, http.StatusServiceUnavailable, "ai_unavailable")
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "baseline.json")
	var stdout, stderr bytes.Buffer
	exitCode := runCLI([]string{
		"-base-url", server.URL,
		"-dataset", frozenDatasetTestPath,
		"-timeout", "1s",
		"-case-delay", "0s",
		"-max-retries", "0",
		"-retry-backoff", "0s",
		"-output", output,
	}, &stdout, &stderr)
	if exitCode == 0 || stderr.Len() != 0 {
		t.Fatalf("exit/stderr = %d / %q", exitCode, stderr.String())
	}
	var got report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.RunStatus != "INCOMPLETE" || got.InfraErrorCases != 30 {
		t.Fatalf("report = %#v", got)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("incomplete run published output: %v", err)
	}
}

func TestExistingOutputIsNeverOverwritten(t *testing.T) {
	output := filepath.Join(t.TempDir(), "baseline.json")
	original := []byte("existing baseline")
	if err := os.WriteFile(output, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishAtomic(output, []byte("replacement")); err == nil {
		t.Fatal("publishAtomic overwrote an existing target")
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("existing output changed: %q", got)
	}
}

func TestInvalidFrozenHashStopsBeforeLiveRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dataset.jsonl")
	if err := os.WriteFile(path, []byte(validDatasetLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writeChatResponse(t, writer, unknownChat())
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	exitCode := runCLI([]string{"-base-url", server.URL, "-dataset", path, "-case-delay", "0s"}, &stdout, &stderr)
	if exitCode == 0 || requests != 0 {
		t.Fatalf("exit/requests = %d / %d", exitCode, requests)
	}
	var got report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || got.RunStatus != "INVALID" || got.HarnessError != "invalid_or_unfrozen_dataset" {
		t.Fatalf("report/error = %#v / %v", got, err)
	}
}
