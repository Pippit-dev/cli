package config

import (
	"time"
)

const (
	DefaultBaseURL              = "https://xyq.jianying.com"
	DefaultHTTPTimeout          = 30 * time.Second
	DefaultAuthTTL              = 30 * time.Second
	DefaultOAuthClientKey       = "mock-cli"
	DefaultOAuthBaseURL         = "https://passport.bytedance.com"
	DefaultAuthStoreServiceName = "pippit-cli"
	SubmitRunPath               = "/api/biz/v1/skill/submit_run"
	GetThreadPath               = "/api/biz/v1/skill/get_thread"
	UploadFilePath              = "/api/biz/v1/skill/upload_file"
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

// Load resolves the built-in runtime config.
func Load() *Config {
	return &Config{
		BaseURL:     DefaultBaseURL,
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
	return &OAuth{
		ClientKey:        DefaultOAuthClientKey,
		BaseURL:          DefaultOAuthBaseURL,
		StoreServiceName: DefaultAuthStoreServiceName,
		Scopes:           []string{"user_info", "aigc_generate"},
	}
}
