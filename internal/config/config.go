package config

import (
	"os"
	"time"
)

const (
	DefaultBaseURL     = "https://xyq.jianying.com"
	DefaultHTTPTimeout = 30 * time.Second
	SubmitRunPath      = "/api/biz/v1/skill/submit_run"
	GetThreadPath      = "/api/biz/v1/skill/get_thread"
	UploadFilePath     = "/api/biz/v1/skill/upload_file"
	envPippitBaseURL   = "PIPPIT_CLI_BASE_URL"
	envXYQOpenAPIBase  = "XYQ_OPENAPI_BASE"
	envXYQBaseURL      = "XYQ_BASE_URL"
)

// Config holds runtime settings selected by the root command and passed down
// into lower layers.
type Config struct {
	BaseURL     string
	HTTPTimeout time.Duration
	Paths       *Paths
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
		Paths: &Paths{
			SubmitRun:  SubmitRunPath,
			GetThread:  GetThreadPath,
			UploadFile: UploadFilePath,
		},
	}
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
