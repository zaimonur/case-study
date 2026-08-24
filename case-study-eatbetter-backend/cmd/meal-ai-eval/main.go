// Command meal-ai-eval measures the frozen MealAI chat dataset against the live v2 HTTP API.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	defaultBaseURL        = "http://localhost:8080"
	defaultDatasetPath    = "data/evaluation/mealai-chat-v1.jsonl"
	defaultTimeout        = 20 * time.Second
	defaultCaseDelay      = 2 * time.Second
	defaultMaxRetries     = 2
	defaultRetryBackoff   = 2 * time.Second
	maxConfiguredDuration = 10 * time.Minute
	maxConfiguredRetries  = 10
)

type cliConfig struct {
	Origin       string
	Dataset      string
	Timeout      time.Duration
	CaseDelay    time.Duration
	MaxRetries   int
	RetryBackoff time.Duration
	Output       string
	ModelLabel   *string
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	config, err := parseCLI(args)
	if err != nil {
		invalid := invalidReport("invalid_cli", "", "", nil)
		return writeAndExit(stdout, stderr, invalid, 2)
	}
	if err := preflightOutput(config.Output); err != nil {
		invalid := invalidReport("invalid_output", config.Origin, "", config.ModelLabel)
		return writeAndExit(stdout, stderr, invalid, 2)
	}
	cases, datasetHash, turnCount, err := loadDataset(config.Dataset)
	if err != nil || datasetHash != frozenDatasetSHA256 {
		invalid := invalidReport("invalid_or_unfrozen_dataset", config.Origin, datasetHash, config.ModelLabel)
		return writeAndExit(stdout, stderr, invalid, 2)
	}

	base := report{
		DatasetVersion: datasetVersion, DatasetSHA256: datasetHash,
		DatasetCaseCount: len(cases), DatasetTurnCount: turnCount,
		FrozenTask6AGitHead: frozenTask6AHead, APIOrigin: config.Origin,
		ConversationVersion: conversationVersion, ModelLabel: config.ModelLabel,
	}
	runner := &evaluator{
		origin: config.Origin, client: &http.Client{Timeout: config.Timeout},
		caseDelay: config.CaseDelay, maxRetries: config.MaxRetries, retryBackoff: config.RetryBackoff,
		wait: time.Sleep, now: time.Now,
	}
	result := runner.evaluate(context.Background(), cases, base)

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		invalid := invalidReport("report_encoding", config.Origin, datasetHash, config.ModelLabel)
		return writeAndExit(stdout, stderr, invalid, 2)
	}
	encoded = append(encoded, '\n')
	if result.RunStatus == "COMPLETE" && config.Output != "" {
		if err := publishAtomic(config.Output, encoded); err != nil {
			result.RunStatus = "INVALID"
			result.HarnessError = "output_publish_failure"
			encoded, _ = json.MarshalIndent(result, "", "  ")
			encoded = append(encoded, '\n')
			if _, writeErr := stdout.Write(encoded); writeErr != nil {
				_, _ = fmt.Fprintln(stderr, "unable to write evaluator report")
			}
			return 2
		}
	}
	if _, err := stdout.Write(encoded); err != nil {
		_, _ = fmt.Fprintln(stderr, "unable to write evaluator report")
		return 2
	}
	if result.RunStatus == "COMPLETE" {
		return 0
	}
	if result.RunStatus == "INVALID" {
		return 2
	}
	return 1
}

func parseCLI(args []string) (cliConfig, error) {
	flags := flag.NewFlagSet("meal-ai-eval", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseURL := flags.String("base-url", defaultBaseURL, "MealAI API origin")
	dataset := flags.String("dataset", defaultDatasetPath, "frozen JSONL dataset")
	timeout := flags.Duration("timeout", defaultTimeout, "per-request timeout")
	caseDelay := flags.Duration("case-delay", defaultCaseDelay, "pacing between logical requests")
	maxRetries := flags.Int("max-retries", defaultMaxRetries, "additional attempts for retryable infrastructure failures")
	retryBackoff := flags.Duration("retry-backoff", defaultRetryBackoff, "fallback retry wait")
	output := flags.String("output", "", "optional COMPLETE report path")
	modelLabel := flags.String("model-label", "", "optional non-secret model metadata")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return cliConfig{}, fmt.Errorf("invalid flags")
	}
	origin, err := normalizeOrigin(*baseURL)
	if err != nil || strings.TrimSpace(*dataset) == "" || *timeout <= 0 || *timeout > maxConfiguredDuration ||
		*caseDelay < 0 || *caseDelay > maxConfiguredDuration || *retryBackoff < 0 || *retryBackoff > maxConfiguredDuration ||
		*maxRetries < 0 || *maxRetries > maxConfiguredRetries {
		return cliConfig{}, fmt.Errorf("invalid configuration")
	}
	label, err := normalizeModelLabel(*modelLabel)
	if err != nil {
		return cliConfig{}, err
	}
	return cliConfig{
		Origin: origin, Dataset: *dataset, Timeout: *timeout, CaseDelay: *caseDelay,
		MaxRetries: *maxRetries, RetryBackoff: *retryBackoff, Output: *output, ModelLabel: label,
	}, nil
}

func normalizeOrigin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		strings.Contains(raw, "#") || parsed.RawPath != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("invalid origin")
	}
	parsed.Path = ""
	return parsed.String(), nil
}

func normalizeModelLabel(raw string) (*string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 128 {
		return nil, fmt.Errorf("invalid model label")
	}
	for _, current := range value {
		if unicode.IsControl(current) {
			return nil, fmt.Errorf("invalid model label")
		}
	}
	return &value, nil
}

func preflightOutput(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("output already exists or cannot be inspected")
	}
	directory := filepath.Dir(path)
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("output directory is invalid")
	}
	return nil
}

func publishAtomic(path string, body []byte) error {
	if err := preflightOutput(path); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".meal-ai-eval-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func invalidReport(kind, origin, datasetHash string, modelLabel *string) report {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return report{
		RunStatus: "INVALID", DatasetVersion: datasetVersion, DatasetSHA256: datasetHash,
		FrozenTask6AGitHead: frozenTask6AHead, StartedAt: now, CompletedAt: now,
		APIOrigin: origin, ConversationVersion: conversationVersion, ModelLabel: modelLabel,
		InfraErrors: []infraDiagnostic{}, Cases: []caseDiagnostic{}, HarnessError: kind,
	}
}

func writeAndExit(stdout, stderr io.Writer, value report, exitCode int) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_, _ = fmt.Fprintln(stderr, "unable to write evaluator report")
		return 2
	}
	return exitCode
}
