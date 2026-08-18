package database

import (
	"context"
	"strings"
	"testing"

	"eatbetter-backend/internal/config"
)

func TestOpenDoesNotExposeInvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	const secret = "super-secret-password"
	_, err := Open(context.Background(), config.Database{
		URL: "postgres://eatbetter:" + secret + "@%zz:5432/eatbetter",
	})
	if err == nil {
		t.Fatal("Open() error = nil, want invalid DATABASE_URL error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Open() error exposed a database secret: %v", err)
	}
}
