package config

import (
	"os"
	"strings"
	"time"
)

const (
	DefaultBaseURL              = "https://xyq.jianying.com"
	DefaultHTTPTimeout          = 30 * time.Second
	DefaultAuthTTL              = 30 * time.Second
	DefaultAuthStoreServiceName = "pippit-cli"
	SubmitRunPath               = "/api/biz/v1/skill/submit_run"
	GetThreadPath               = "/api/biz/v1/skill/get_thread"
	UploadFilePath              = "/api/biz/v1/skill/upload_file"
	envPippitBaseURL            = "PIPPIT_CLI_BASE_URL"
	envXYQOpenAPIBase           = "XYQ_OPENAPI_BASE"
	envXYQBaseURL               = "XYQ_BASE_URL"
	envPippitOAuthClientKey     = "PIPPIT_OAUTH_CLIENT_KEY"
	envPippitOAuthBaseURL       = "PIPPIT_OAUTH_BASE_URL"
	envPippitOAuthStoreName     = "PIPPIT_OAUTH_STORE_SERVICE_NAME"
	envPippitOAuthScopes        = "PIPPIT_OAUTH_SCOPES"
)

// Config holds runtime settings selected by the root command and passed down
// into lower layers.
type Config struct {
	BaseURL     string
	HTTPTimeout time.Duration
	AuthTTL     time.Duration
	OAuth       *OAuth
	Paths       *Paths
}

type OAuth struct {
	ClientKey        string
	BaseURL          string
	StoreServiceName string
	Scopes           []string
}

type Paths struct {
	SubmitRun  string
	GetThread  string
	UploadFile string
}

// Load resolves the active config. For now the only selection condition is
// environment; this is the intended extension point for future scene/env/brand
// switching.
func Load() *Config {
	return &Config{
		BaseURL:     resolveBaseURL(),
		HTTPTimeout: DefaultHTTPTimeout,
		AuthTTL:     DefaultAuthTTL,
		OAuth:       resolveOAuth(),
		Paths: &Paths{
			SubmitRun:  SubmitRunPath,
			GetThread:  GetThreadPath,
			UploadFile: UploadFilePath,
		},
	}
}

func resolveOAuth() *OAuth {
	storeServiceName := strings.TrimSpace(os.Getenv(envPippitOAuthStoreName))
	if storeServiceName == "" {
		storeServiceName = DefaultAuthStoreServiceName
	}
	return &OAuth{
		ClientKey:        strings.TrimSpace(os.Getenv(envPippitOAuthClientKey)),
		BaseURL:          strings.TrimSpace(os.Getenv(envPippitOAuthBaseURL)),
		StoreServiceName: storeServiceName,
		Scopes:           splitScopes(os.Getenv(envPippitOAuthScopes)),
	}
}

func splitScopes(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	scopes := make([]string, 0, len(parts))
	for _, part := range parts {
		if scope := strings.TrimSpace(part); scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func resolveBaseURL() string {
	if v := os.Getenv(envPippitBaseURL); v != "" {
		return v
	}
	if v := os.Getenv(envXYQOpenAPIBase); v != "" {
		return v
	}
	if v := os.Getenv(envXYQBaseURL); v != "" {
		return v
	}
	return DefaultBaseURL
}
