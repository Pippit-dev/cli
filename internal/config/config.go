package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultBaseURL              = "https://xyq.jianying.com"
	DefaultHTTPTimeout          = 30 * time.Minute
	DefaultAuthTTL              = 30 * time.Second
	DefaultAuthStoreServiceName = "pippit-cli"
	SubmitRunPath               = "/api/biz/v1/skill/submit_run"
	GetThreadPath               = "/api/biz/v1/skill/get_thread"
	UploadFilePath              = "/api/biz/v1/skill/upload_file"
	ListThreadFilePath          = "/api/biz/v1/skill/list_thread_file"
	EnvXYQAccessKey             = "XYQ_ACCESS_KEY"
	EnvPPEEnv                   = "PIPPIT_CLI_PPE_ENV"
)

var ppeEnvPattern = regexp.MustCompile(`^ppe_[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Config holds runtime settings selected by the root command and passed down
// into lower layers.
type Config struct {
	BaseURL     string
	HTTPTimeout time.Duration
	AuthTTL     time.Duration
	AccessKey   string
	PPEEnv      string
	Paths       *Paths
}

type Paths struct {
	SubmitRun      string
	GetThread      string
	UploadFile     string
	ListThreadFile string
}

// Load resolves the built-in runtime config.
func Load() *Config {
	return &Config{
		BaseURL:     DefaultBaseURL,
		HTTPTimeout: DefaultHTTPTimeout,
		AuthTTL:     DefaultAuthTTL,
		AccessKey:   strings.TrimSpace(os.Getenv(EnvXYQAccessKey)),
		PPEEnv:      strings.TrimSpace(os.Getenv(EnvPPEEnv)),
		Paths: &Paths{
			SubmitRun:      SubmitRunPath,
			GetThread:      GetThreadPath,
			UploadFile:     UploadFilePath,
			ListThreadFile: ListThreadFilePath,
		},
	}
}

// NormalizePPEEnv validates an optional PPE lane name before it can be sent in
// request headers. Empty selects production. Keeping the accepted character
// set narrow also prevents malformed header values from reaching net/http.
func NormalizePPEEnv(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !ppeEnvPattern.MatchString(value) {
		return "", fmt.Errorf("PPE 环境 %q 非法：必须以 ppe_ 开头，且只能包含字母、数字、点、下划线或连字符", value)
	}
	return value, nil
}
