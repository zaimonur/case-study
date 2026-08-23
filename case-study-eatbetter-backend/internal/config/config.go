package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAppEnvironment      = "development"
	defaultHTTPPort            = 8080
	defaultReadHeaderTimeout   = 5 * time.Second
	defaultIdleTimeout         = 60 * time.Second
	defaultShutdownTimeout     = 10 * time.Second
	defaultDatabaseMaxConns    = int32(10)
	defaultDatabaseMinConns    = int32(1)
	defaultDatabaseMaxLifetime = 30 * time.Minute
	defaultDatabasePingTimeout = 2 * time.Second
	defaultGroqModel           = "openai/gpt-oss-120b"
	defaultGroqTimeout         = 10 * time.Second
	defaultGeminiModel         = "gemini-2.5-flash"
	defaultGeminiTimeout       = 15 * time.Second
)

// Config contains all runtime configuration needed by the API process.
type Config struct {
	AppEnvironment string
	LogLevel       slog.Level
	HTTP           HTTP
	Database       Database
	Groq           Groq
	Gemini         Gemini
}

// HTTP contains HTTP server lifecycle settings.
type HTTP struct {
	Port              int
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

// Address returns the TCP address used by the HTTP server.
func (c HTTP) Address() string {
	return fmt.Sprintf(":%d", c.Port)
}

// Database contains PostgreSQL pool and health-check settings.
type Database struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	PingTimeout     time.Duration
}

// Groq contains optional text-extraction provider configuration. APIKey may be
// empty so deployments that do not use AI extraction can still start.
type Groq struct {
	APIKey  string
	Model   string
	Timeout time.Duration
}

// Gemini contains optional image-extraction provider configuration. APIKey may
// be empty so deployments that do not use image extraction can still start.
type Gemini struct {
	APIKey  string
	Model   string
	Timeout time.Duration
}

// Load reads and validates runtime configuration from the environment.
func Load() (Config, error) {
	return load(os.LookupEnv)
}

type lookupEnv func(string) (string, bool)

func load(lookup lookupEnv) (Config, error) {
	databaseURL, ok := lookup("DATABASE_URL")
	if !ok || strings.TrimSpace(databaseURL) == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	appEnvironment := stringValue(lookup, "APP_ENV", defaultAppEnvironment)
	if strings.TrimSpace(appEnvironment) == "" {
		return Config{}, fmt.Errorf("APP_ENV must not be empty")
	}

	logLevel, err := parseLogLevel(stringValue(lookup, "LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, err
	}

	httpPort, err := intValue(lookup, "HTTP_PORT", defaultHTTPPort, 1, 65535)
	if err != nil {
		return Config{}, err
	}
	readHeaderTimeout, err := durationValue(lookup, "HTTP_READ_HEADER_TIMEOUT", defaultReadHeaderTimeout)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := durationValue(lookup, "HTTP_IDLE_TIMEOUT", defaultIdleTimeout)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := durationValue(lookup, "SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		return Config{}, err
	}

	maxConns, err := positiveInt32Value(lookup, "DB_MAX_CONNS", defaultDatabaseMaxConns)
	if err != nil {
		return Config{}, err
	}
	minConns, err := nonNegativeInt32Value(lookup, "DB_MIN_CONNS", defaultDatabaseMinConns)
	if err != nil {
		return Config{}, err
	}
	if minConns > maxConns {
		return Config{}, fmt.Errorf("DB_MIN_CONNS must be less than or equal to DB_MAX_CONNS")
	}
	maxConnLifetime, err := durationValue(lookup, "DB_MAX_CONN_LIFETIME", defaultDatabaseMaxLifetime)
	if err != nil {
		return Config{}, err
	}
	pingTimeout, err := durationValue(lookup, "DB_PING_TIMEOUT", defaultDatabasePingTimeout)
	if err != nil {
		return Config{}, err
	}

	groqAPIKey := stringValue(lookup, "GROQ_API_KEY", "")
	groqModel := strings.TrimSpace(stringValue(lookup, "GROQ_MODEL", defaultGroqModel))
	if groqModel == "" {
		return Config{}, fmt.Errorf("GROQ_MODEL must not be empty")
	}
	groqTimeout, err := durationValue(lookup, "GROQ_TIMEOUT", defaultGroqTimeout)
	if err != nil {
		return Config{}, err
	}

	geminiAPIKey := stringValue(lookup, "GEMINI_API_KEY", "")
	geminiModel := strings.TrimSpace(stringValue(lookup, "GEMINI_MODEL", defaultGeminiModel))
	if geminiModel == "" {
		return Config{}, fmt.Errorf("GEMINI_MODEL must not be empty")
	}
	geminiTimeout, err := durationValue(lookup, "GEMINI_TIMEOUT", defaultGeminiTimeout)
	if err != nil {
		return Config{}, err
	}

	return Config{
		AppEnvironment: appEnvironment,
		LogLevel:       logLevel,
		HTTP: HTTP{
			Port:              httpPort,
			ReadHeaderTimeout: readHeaderTimeout,
			IdleTimeout:       idleTimeout,
			ShutdownTimeout:   shutdownTimeout,
		},
		Database: Database{
			URL:             databaseURL,
			MaxConns:        maxConns,
			MinConns:        minConns,
			MaxConnLifetime: maxConnLifetime,
			PingTimeout:     pingTimeout,
		},
		Groq: Groq{
			APIKey:  groqAPIKey,
			Model:   groqModel,
			Timeout: groqTimeout,
		},
		Gemini: Gemini{
			APIKey:  geminiAPIKey,
			Model:   geminiModel,
			Timeout: geminiTimeout,
		},
	}, nil
}

func stringValue(lookup lookupEnv, name, defaultValue string) string {
	if value, ok := lookup(name); ok {
		return value
	}
	return defaultValue
}

func intValue(lookup lookupEnv, name string, defaultValue, minimum, maximum int) (int, error) {
	raw := stringValue(lookup, name, strconv.Itoa(defaultValue))
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return value, nil
}

func positiveInt32Value(lookup lookupEnv, name string, defaultValue int32) (int32, error) {
	raw := stringValue(lookup, name, strconv.FormatInt(int64(defaultValue), 10))
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive 32-bit integer", name)
	}
	return int32(value), nil
}

func nonNegativeInt32Value(lookup lookupEnv, name string, defaultValue int32) (int32, error) {
	raw := stringValue(lookup, name, strconv.FormatInt(int64(defaultValue), 10))
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative 32-bit integer", name)
	}
	return int32(value), nil
}

func durationValue(lookup lookupEnv, name string, defaultValue time.Duration) (time.Duration, error) {
	raw := stringValue(lookup, name, defaultValue.String())
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration", name)
	}
	return value, nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("LOG_LEVEL must be one of debug, info, warn, or error")
	}
}
