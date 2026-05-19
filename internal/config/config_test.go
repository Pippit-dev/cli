package config

import "testing"

func TestLoadUsesDefaultConfig(t *testing.T) {
	t.Setenv(envPippitBaseURL, "")
	t.Setenv(envXYQOpenAPIBase, "")
	t.Setenv(envXYQBaseURL, "")
	t.Setenv(envPippitOAuthClientKey, "")
	t.Setenv(envPippitOAuthBaseURL, "")
	t.Setenv(envPippitOAuthStoreName, "")
	t.Setenv(envPippitOAuthScopes, "")

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
	if cfg.OAuth.StoreServiceName != DefaultAuthStoreServiceName {
		t.Fatalf("OAuth.StoreServiceName = %q, want %q", cfg.OAuth.StoreServiceName, DefaultAuthStoreServiceName)
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

func TestLoadOAuthConfigFromEnv(t *testing.T) {
	t.Setenv(envPippitOAuthClientKey, "client-key")
	t.Setenv(envPippitOAuthBaseURL, "https://passport.example")
	t.Setenv(envPippitOAuthStoreName, "store-name")
	t.Setenv(envPippitOAuthScopes, "user_info,aigc_generate extra_scope")

	cfg := Load()
	if cfg.OAuth.ClientKey != "client-key" {
		t.Fatalf("OAuth.ClientKey = %q, want client-key", cfg.OAuth.ClientKey)
	}
	if cfg.OAuth.BaseURL != "https://passport.example" {
		t.Fatalf("OAuth.BaseURL = %q, want passport base URL", cfg.OAuth.BaseURL)
	}
	if cfg.OAuth.StoreServiceName != "store-name" {
		t.Fatalf("OAuth.StoreServiceName = %q, want store-name", cfg.OAuth.StoreServiceName)
	}
	want := []string{"user_info", "aigc_generate", "extra_scope"}
	if len(cfg.OAuth.Scopes) != len(want) {
		t.Fatalf("OAuth.Scopes = %#v, want %#v", cfg.OAuth.Scopes, want)
	}
	for i := range want {
		if cfg.OAuth.Scopes[i] != want[i] {
			t.Fatalf("OAuth.Scopes = %#v, want %#v", cfg.OAuth.Scopes, want)
		}
	}
}
