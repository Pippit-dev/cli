package config

import "testing"

func TestLoadUsesDefaultConfig(t *testing.T) {
	t.Setenv(EnvXYQAccessKey, "")
	t.Setenv(EnvPPEEnv, "")
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
	if cfg.PPEEnv != "" {
		t.Fatalf("PPEEnv = %q, want empty", cfg.PPEEnv)
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
}

func TestLoadReadsAccessKey(t *testing.T) {
	t.Setenv(EnvXYQAccessKey, " test-token ")
	cfg := Load()
	if cfg.AccessKey != "test-token" {
		t.Fatalf("AccessKey = %q, want trimmed token", cfg.AccessKey)
	}
}

func TestLoadReadsPPEEnv(t *testing.T) {
	t.Setenv(EnvPPEEnv, " ppe_cli_canvas_ak ")
	cfg := Load()
	if cfg.PPEEnv != "ppe_cli_canvas_ak" {
		t.Fatalf("PPEEnv = %q, want trimmed PPE lane", cfg.PPEEnv)
	}
}

func TestNormalizePPEEnv(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "production", value: "", want: ""},
		{name: "trimmed", value: "  ppe_cli-canvas.1  ", want: "ppe_cli-canvas.1"},
		{name: "missing prefix", value: "cli_canvas", wantErr: true},
		{name: "header injection", value: "ppe_canvas\r\nx-evil: 1", wantErr: true},
		{name: "space", value: "ppe_canvas lane", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePPEEnv(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizePPEEnv(%q) error = nil, want error", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePPEEnv(%q) error = %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizePPEEnv(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
