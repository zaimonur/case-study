package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoadUsesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapLookup(map[string]string{
		"DATABASE_URL": "postgres://user:password@localhost:5432/eatbetter",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.AppEnvironment != "development" {
		t.Fatalf("AppEnvironment = %q, want development", cfg.AppEnvironment)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.HTTP.Port != 8080 {
		t.Fatalf("HTTP.Port = %d, want 8080", cfg.HTTP.Port)
	}
	if cfg.HTTP.ShutdownTimeout != 10*time.Second {
		t.Fatalf("HTTP.ShutdownTimeout = %s, want 10s", cfg.HTTP.ShutdownTimeout)
	}
	if cfg.Database.MaxConns != 10 || cfg.Database.MinConns != 1 {
		t.Fatalf("database pool = min %d max %d, want min 1 max 10", cfg.Database.MinConns, cfg.Database.MaxConns)
	}
	if cfg.Database.PingTimeout != 2*time.Second {
		t.Fatalf("Database.PingTimeout = %s, want 2s", cfg.Database.PingTimeout)
	}
	if cfg.Groq.APIKey != "" {
		t.Fatalf("Groq.APIKey = %q, want empty", cfg.Groq.APIKey)
	}
	if cfg.Groq.Model != "openai/gpt-oss-120b" {
		t.Fatalf("Groq.Model = %q, want openai/gpt-oss-120b", cfg.Groq.Model)
	}
	if cfg.Groq.Timeout != 10*time.Second {
		t.Fatalf("Groq.Timeout = %s, want 10s", cfg.Groq.Timeout)
	}
	if cfg.Gemini.APIKey != "" {
		t.Fatalf("Gemini.APIKey = %q, want empty", cfg.Gemini.APIKey)
	}
	if cfg.Gemini.Model != "gemini-2.5-flash" {
		t.Fatalf("Gemini.Model = %q, want gemini-2.5-flash", cfg.Gemini.Model)
	}
	if cfg.Gemini.Timeout != 15*time.Second {
		t.Fatalf("Gemini.Timeout = %s, want 15s", cfg.Gemini.Timeout)
	}
}

func TestLoadReadsOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapLookup(map[string]string{
		"DATABASE_URL":             "postgres://localhost/eatbetter",
		"APP_ENV":                  "test",
		"LOG_LEVEL":                "debug",
		"HTTP_PORT":                "9090",
		"HTTP_READ_HEADER_TIMEOUT": "3s",
		"HTTP_IDLE_TIMEOUT":        "45s",
		"SHUTDOWN_TIMEOUT":         "12s",
		"DB_MAX_CONNS":             "20",
		"DB_MIN_CONNS":             "2",
		"DB_MAX_CONN_LIFETIME":     "1h",
		"DB_PING_TIMEOUT":          "750ms",
		"GROQ_API_KEY":             "synthetic-test-key",
		"GROQ_MODEL":               "configured-model",
		"GROQ_TIMEOUT":             "1500ms",
		"GEMINI_API_KEY":           "synthetic-gemini-test-key",
		"GEMINI_MODEL":             "configured-gemini-model",
		"GEMINI_TIMEOUT":           "2500ms",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.AppEnvironment != "test" || cfg.LogLevel != slog.LevelDebug || cfg.HTTP.Port != 9090 {
		t.Fatalf("unexpected application overrides: %+v", cfg)
	}
	if cfg.Database.MaxConns != 20 || cfg.Database.MinConns != 2 {
		t.Fatalf("unexpected database pool overrides: %+v", cfg.Database)
	}
	if cfg.Database.MaxConnLifetime != time.Hour || cfg.Database.PingTimeout != 750*time.Millisecond {
		t.Fatalf("unexpected database duration overrides: %+v", cfg.Database)
	}
	if cfg.Groq.APIKey != "synthetic-test-key" || cfg.Groq.Model != "configured-model" || cfg.Groq.Timeout != 1500*time.Millisecond {
		t.Fatalf("unexpected Groq overrides: model=%q timeout=%s", cfg.Groq.Model, cfg.Groq.Timeout)
	}
	if cfg.Gemini.APIKey != "synthetic-gemini-test-key" || cfg.Gemini.Model != "configured-gemini-model" || cfg.Gemini.Timeout != 2500*time.Millisecond {
		t.Fatalf("unexpected Gemini overrides: model=%q timeout=%s", cfg.Gemini.Model, cfg.Gemini.Timeout)
	}
}

func TestLoadAllowsExplicitlyEmptyGeminiAPIKey(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapLookup(map[string]string{
		"DATABASE_URL":   "postgres://localhost/eatbetter",
		"GEMINI_API_KEY": "",
	}))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Gemini.APIKey != "" {
		t.Fatalf("Gemini.APIKey = %q, want empty", cfg.Gemini.APIKey)
	}
}

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(nil))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("error = %v, want DATABASE_URL validation error", err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		overrides map[string]string
		wantError string
	}{
		{name: "empty environment", overrides: map[string]string{"APP_ENV": ""}, wantError: "APP_ENV"},
		{name: "invalid log level", overrides: map[string]string{"LOG_LEVEL": "trace"}, wantError: "LOG_LEVEL"},
		{name: "invalid port", overrides: map[string]string{"HTTP_PORT": "70000"}, wantError: "HTTP_PORT"},
		{name: "invalid duration", overrides: map[string]string{"SHUTDOWN_TIMEOUT": "0s"}, wantError: "SHUTDOWN_TIMEOUT"},
		{name: "invalid max connections", overrides: map[string]string{"DB_MAX_CONNS": "0"}, wantError: "DB_MAX_CONNS"},
		{name: "invalid min connections", overrides: map[string]string{"DB_MIN_CONNS": "-1"}, wantError: "DB_MIN_CONNS"},
		{name: "min exceeds max", overrides: map[string]string{"DB_MAX_CONNS": "2", "DB_MIN_CONNS": "3"}, wantError: "DB_MIN_CONNS"},
		{name: "blank Groq model", overrides: map[string]string{"GROQ_MODEL": "  "}, wantError: "GROQ_MODEL"},
		{name: "invalid Groq timeout", overrides: map[string]string{"GROQ_TIMEOUT": "later"}, wantError: "GROQ_TIMEOUT"},
		{name: "zero Groq timeout", overrides: map[string]string{"GROQ_TIMEOUT": "0s"}, wantError: "GROQ_TIMEOUT"},
		{name: "negative Groq timeout", overrides: map[string]string{"GROQ_TIMEOUT": "-1s"}, wantError: "GROQ_TIMEOUT"},
		{name: "blank Gemini model", overrides: map[string]string{"GEMINI_MODEL": "  "}, wantError: "GEMINI_MODEL"},
		{name: "invalid Gemini timeout", overrides: map[string]string{"GEMINI_TIMEOUT": "later"}, wantError: "GEMINI_TIMEOUT"},
		{name: "zero Gemini timeout", overrides: map[string]string{"GEMINI_TIMEOUT": "0s"}, wantError: "GEMINI_TIMEOUT"},
		{name: "negative Gemini timeout", overrides: map[string]string{"GEMINI_TIMEOUT": "-1s"}, wantError: "GEMINI_TIMEOUT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			values := map[string]string{"DATABASE_URL": "postgres://localhost/eatbetter"}
			for key, value := range tt.overrides {
				values[key] = value
			}

			_, err := load(mapLookup(values))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want error containing %q", err, tt.wantError)
			}
		})
	}
}

func TestLoadErrorsNeverContainGroqAPIKey(t *testing.T) {
	t.Parallel()

	const secret = "synthetic-sensitive-key-content"
	_, err := load(mapLookup(map[string]string{
		"DATABASE_URL": "postgres://localhost/eatbetter",
		"GROQ_API_KEY": secret,
		"GROQ_MODEL":   " ",
	}))
	if err == nil {
		t.Fatal("load config succeeded, want error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("configuration error exposed API key: %v", err)
	}
}

func TestLoadErrorsNeverContainGeminiAPIKey(t *testing.T) {
	t.Parallel()

	const secret = "synthetic-sensitive-gemini-key-content"
	_, err := load(mapLookup(map[string]string{
		"DATABASE_URL":   "postgres://localhost/eatbetter",
		"GEMINI_API_KEY": secret,
		"GEMINI_MODEL":   " ",
	}))
	if err == nil {
		t.Fatal("load config succeeded, want error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("configuration error exposed API key: %v", err)
	}
}

func mapLookup(values map[string]string) lookupEnv {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
