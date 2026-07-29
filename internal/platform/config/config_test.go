package config_test

import (
	"testing"

	"github.com/AhmadEbeid/passwordless-auth-service/internal/platform/config"
)

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is unset, got nil")
	}
}

func TestLoad_DefaultsAndValues(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/authsvc")
	t.Setenv("HTTP_PORT", "")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("APP_ENV", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://localhost:5432/authsvc" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort default = %q, want 8080", cfg.HTTPPort)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.Env != "development" {
		t.Errorf("Env default = %q, want development", cfg.Env)
	}
}

// A blank link secret makes the server improvise a per-process one. That is
// survivable in development and silently corrupting in production, where a
// second replica or a restart invalidates in-flight Google signups.
func TestLoad_ProductionRequiresGoogleLinkSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/authsvc")
	t.Setenv("APP_ENV", "production")
	t.Setenv("GOOGLE_LINK_SECRET", "")

	if _, err := config.Load(); err == nil {
		t.Fatal("production with no GOOGLE_LINK_SECRET loaded without error")
	}
}

func TestLoad_ProductionAcceptsConfiguredGoogleLinkSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/authsvc")
	t.Setenv("APP_ENV", "production")
	t.Setenv("GOOGLE_LINK_SECRET", "a-real-secret")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.IsProduction() {
		t.Error("IsProduction() = false for APP_ENV=production")
	}
	if cfg.GoogleLinkSecret != "a-real-secret" {
		t.Errorf("GoogleLinkSecret = %q", cfg.GoogleLinkSecret)
	}
}

// The same omission must not block development, or nobody can run the service
// locally without inventing a secret first.
func TestLoad_DevelopmentToleratesMissingGoogleLinkSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/authsvc")
	t.Setenv("APP_ENV", "")
	t.Setenv("GOOGLE_LINK_SECRET", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("development with no GOOGLE_LINK_SECRET failed to load: %v", err)
	}
	if cfg.IsProduction() {
		t.Error("IsProduction() = true for the default env")
	}
}
