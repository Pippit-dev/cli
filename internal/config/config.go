package config

import (
	"os"
	"strings"
	"time"
)

const (
	DefaultBaseURL              = "https://xyq.jianying.com"
	DefaultHTTPTimeout          = 30 * time.Minute
	DefaultAuthTTL              = 30 * time.Second
	DefaultAuthStoreServiceName = "pippit-cli"
	SubmitRunPath               = "/api/biz/v1/skill/submit_run"
	GetCreditBalancePath        = "/api/biz/v1/skill/get_credit_balance"
	GetThreadPath               = "/api/biz/v1/skill/get_thread"
	UploadFilePath              = "/api/biz/v1/skill/upload_file"
	ListThreadFilePath          = "/api/biz/v1/skill/list_thread_file"
	EnvXYQAccessKey             = "XYQ_ACCESS_KEY"
)

// Config holds runtime settings selected by the root command and passed down
// into lower layers.
type Config struct {
	BaseURL     string
	HTTPTimeout time.Duration
	AuthTTL     time.Duration
	AccessKey   string
	Paths       *Paths
}

type Paths struct {
	SubmitRun        string
	GetCreditBalance string
	GetThread        string
	UploadFile       string
	ListThreadFile   string
}

// Load resolves the built-in runtime config.
func Load() *Config {
	return &Config{
		BaseURL:     DefaultBaseURL,
		HTTPTimeout: DefaultHTTPTimeout,
		AuthTTL:     DefaultAuthTTL,
		AccessKey:   strings.TrimSpace(os.Getenv(EnvXYQAccessKey)),
		Paths: &Paths{
			SubmitRun:        SubmitRunPath,
			GetCreditBalance: GetCreditBalancePath,
			GetThread:        GetThreadPath,
			UploadFile:       UploadFilePath,
			ListThreadFile:   ListThreadFilePath,
		},
	}
}
