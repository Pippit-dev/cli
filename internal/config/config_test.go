package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile(t *testing.T) {
	// The retired PPE environment variable must not override the runtime file.
	t.Setenv("PIPPIT_CLI_PPE_ENV", "ppe_ignored")
	for _, tc := range []struct {
		name, content, wantURL, wantEnv string
		missing, wantErr                bool
	}{
		{name: "missing", missing: true, wantURL: DefaultBaseURL},
		{name: "empty", content: "", wantURL: DefaultBaseURL},
		{name: "production", content: `xyq_api_url = "https://xyq.jianying.com"` + "\n" + `x-tt-env = ""`, wantURL: DefaultBaseURL},
		{name: "ppe", content: "# local routing\nxyq_api_url = 'https://xyq.jianying.com/'\nx-tt-env = ' ppe_self_testin_5691a3 '", wantURL: DefaultBaseURL, wantEnv: "ppe_self_testin_5691a3"},
		{name: "invalid toml", content: `x-tt-env = "`, wantErr: true},
		{name: "invalid type", content: `x-tt-env = 1`, wantErr: true},
		{name: "invalid ppe", content: `x-tt-env = "prod"`, wantErr: true},
		{name: "typo", content: `x-tt_env = "ppe_fixture"`, wantErr: true},
		{name: "invalid url", content: `xyq_api_url = "xyq.jianying.com"`, wantErr: true},
		{name: "url credentials", content: `xyq_api_url = "https://user:pass@xyq.jianying.com"`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			if !tc.missing {
				if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := LoadFile(path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("LoadFile() error = %v, wantErr=%v", err, tc.wantErr)
			}
			if err == nil && (cfg.BaseURL != tc.wantURL || cfg.PPEEnv != tc.wantEnv) {
				t.Fatalf("routing = %q, %q; want %q, %q", cfg.BaseURL, cfg.PPEEnv, tc.wantURL, tc.wantEnv)
			}
		})
	}
}

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
	if cfg.Paths.GetAvailableModelList != GetAvailableModelListPath {
		t.Fatalf("GetAvailableModelList path = %q, want %q", cfg.Paths.GetAvailableModelList, GetAvailableModelListPath)
	}
}

func TestLoadReadsAccessKey(t *testing.T) {
	t.Setenv(EnvXYQAccessKey, " test-token ")
	cfg := Load()
	if cfg.AccessKey != "test-token" {
		t.Fatalf("AccessKey = %q, want trimmed token", cfg.AccessKey)
	}
}

func TestLoadIgnoresUntrustedBaseURLEnvironment(t *testing.T) {
	for _, name := range []string{"XYQ_OPENAPI_BASE", "XYQ_BASE_URL"} {
		t.Setenv(name, "https://untrusted.example")
	}
	cfg := Load()
	if cfg.BaseURL != "https://xyq.jianying.com" {
		t.Fatalf("BaseURL must remain the production HTTPS origin")
	}
}
