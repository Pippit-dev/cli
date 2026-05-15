package config

import "testing"

func TestLoadUsesDefaultConfig(t *testing.T) {
	t.Setenv(envPippitBaseURL, "")
	t.Setenv(envXYQOpenAPIBase, "")
	t.Setenv(envXYQBaseURL, "")

	cfg := Load()
	if cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, DefaultBaseURL)
	}
	if cfg.HTTPTimeout != DefaultHTTPTimeout {
		t.Fatalf("HTTPTimeout = %s, want %s", cfg.HTTPTimeout, DefaultHTTPTimeout)
	}
	if cfg.Paths.SubmitRun != SubmitRunPath {
		t.Fatalf("SubmitRun path = %q, want %q", cfg.Paths.SubmitRun, SubmitRunPath)
	}
}

func TestLoadBaseURLPrecedence(t *testing.T) {
	t.Setenv(envPippitBaseURL, "https://pippit.example")
	t.Setenv(envXYQOpenAPIBase, "https://openapi.example")
	t.Setenv(envXYQBaseURL, "https://xyq.example")

	cfg := Load()
	if cfg.BaseURL != "https://pippit.example" {
		t.Fatalf("BaseURL = %q, want PIPPIT_CLI_BASE_URL value", cfg.BaseURL)
	}
}

func TestLoadFallsBackToLegacyBaseURLEnvs(t *testing.T) {
	t.Setenv(envPippitBaseURL, "")
	t.Setenv(envXYQOpenAPIBase, "https://openapi.example")
	t.Setenv(envXYQBaseURL, "https://xyq.example")

	cfg := Load()
	if cfg.BaseURL != "https://openapi.example" {
		t.Fatalf("BaseURL = %q, want XYQ_OPENAPI_BASE value", cfg.BaseURL)
	}
}
