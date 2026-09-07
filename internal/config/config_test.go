package config

import "testing"

func TestLoadUsesDefaultConfig(t *testing.T) {
	t.Setenv(EnvXYQAccessKey, "")
	cfg := Load()
	if cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, DefaultBaseURL)
	}
	if cfg.HTTPTimeout != DefaultHTTPTimeout {
		t.Fatalf("HTTPTimeout = %s, want %s", cfg.HTTPTimeout, DefaultHTTPTimeout)
	}
	if cfg.AuthTTL != DefaultAuthTTL {
		t.Fatalf("AuthTTL = %s, want %s", cfg.AuthTTL, DefaultAuthTTL)
	}
	if cfg.AccessKey != "" {
		t.Fatalf("AccessKey = %q, want empty", cfg.AccessKey)
	}
	if cfg.Paths.SubmitRun != SubmitRunPath {
		t.Fatalf("SubmitRun path = %q, want %q", cfg.Paths.SubmitRun, SubmitRunPath)
	}
	if cfg.Paths.GetCreditBalance != GetCreditBalancePath {
		t.Fatalf("GetCreditBalance path = %q, want %q", cfg.Paths.GetCreditBalance, GetCreditBalancePath)
	}
}

func TestLoadReadsAccessKey(t *testing.T) {
	t.Setenv(EnvXYQAccessKey, " test-token ")
	cfg := Load()
	if cfg.AccessKey != "test-token" {
		t.Fatalf("AccessKey = %q, want trimmed token", cfg.AccessKey)
	}
}
