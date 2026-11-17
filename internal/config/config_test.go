package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresAuthToken(t *testing.T) {
	t.Setenv("AUTH_TOKEN", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "AUTH_TOKEN") {
		t.Fatalf("expected AUTH_TOKEN error, got %v", err)
	}
}

func TestLoadRejectsShortAuthToken(t *testing.T) {
	t.Setenv("AUTH_TOKEN", "short")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for short token")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("AUTH_TOKEN", "test-token-value")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("unexpected addr %q", cfg.Addr)
	}
	if cfg.Environment != "development" {
		t.Fatalf("unexpected env %q", cfg.Environment)
	}
	if cfg.ShutdownTimeout.Seconds() != 10 {
		t.Fatalf("unexpected shutdown timeout %v", cfg.ShutdownTimeout)
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	t.Setenv("AUTH_TOKEN", "test-token-value")
	t.Setenv("LOG_LEVEL", "verbose")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid log level")
	}
}
