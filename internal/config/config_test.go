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
	if cfg.OAuth.ClientKey != DefaultOAuthClientKey {
		t.Fatalf("OAuth.ClientKey = %q, want %q", cfg.OAuth.ClientKey, DefaultOAuthClientKey)
	}
	if cfg.OAuth.StoreServiceName != DefaultAuthStoreServiceName {
		t.Fatalf("OAuth.StoreServiceName = %q, want %q", cfg.OAuth.StoreServiceName, DefaultAuthStoreServiceName)
	}
	if cfg.OAuth.BaseURL != DefaultOAuthBaseURL {
		t.Fatalf("OAuth.BaseURL = %q, want %q", cfg.OAuth.BaseURL, DefaultOAuthBaseURL)
	}
	wantScopes := []string{"user_info", "aigc_generate"}
	if len(cfg.OAuth.Scopes) != len(wantScopes) {
		t.Fatalf("OAuth.Scopes = %#v, want %#v", cfg.OAuth.Scopes, wantScopes)
	}
	for i := range wantScopes {
		if cfg.OAuth.Scopes[i] != wantScopes[i] {
			t.Fatalf("OAuth.Scopes = %#v, want %#v", cfg.OAuth.Scopes, wantScopes)
		}
	}
	if cfg.Paths.SubmitRun != SubmitRunPath {
		t.Fatalf("SubmitRun path = %q, want %q", cfg.Paths.SubmitRun, SubmitRunPath)
	}
	if cfg.Paths.GenerateVideoSubmitRun != AgentSubmitRunPath {
		t.Fatalf("GenerateVideoSubmitRun path = %q, want %q", cfg.Paths.GenerateVideoSubmitRun, AgentSubmitRunPath)
	}
}

func TestLoadReadsAccessKey(t *testing.T) {
	t.Setenv(EnvXYQAccessKey, " test-token ")
	cfg := Load()
	if cfg.AccessKey != "test-token" {
		t.Fatalf("AccessKey = %q, want trimmed token", cfg.AccessKey)
	}
}
