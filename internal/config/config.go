package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	DefaultBaseURL               = "https://xyq.jianying.com"
	DefaultHTTPTimeout           = 30 * time.Minute
	DefaultAuthTTL               = 30 * time.Second
	DefaultAuthStoreServiceName  = "pippit-cli"
	SubmitRunPath                = "/api/biz/v1/skill/submit_run"
	SubmitMarketingRunPath       = "/api/biz/v1/agent/submit_marketing_run"
	QueryGenerateVideoResultPath = "/api/biz/v1/agent/query_generate_video_result"
	GetCreditBalancePath         = "/api/biz/v1/skill/get_credit_balance"
	GetLiteTeamListPath          = "/api/biz/v1/skill/get_lite_team_list"
	GetAvailableModelListPath    = "/api/biz/v1/skill/get_available_model_list"
	GetThreadPath                = "/api/biz/v1/skill/get_thread"
	UploadFilePath               = "/api/biz/v1/skill/upload_file"
	ListThreadFilePath           = "/api/biz/v1/skill/list_thread_file"
	EnvXYQAccessKey              = "XYQ_ACCESS_KEY"
	FileName                     = "config.toml"
)

// Config holds runtime settings selected by the root command and passed down
// into lower layers.
type Config struct {
	BaseURL     string
	PPEEnv      string
	HTTPTimeout time.Duration
	AuthTTL     time.Duration
	AccessKey   string
	Paths       *Paths
}

// LoadFile reads runtime overrides from the specified TOML file. A missing file
// preserves the defaults used by installed CLI versions outside the repository.
func LoadFile(path string) (*Config, error) {
	cfg := Load()
	var values struct {
		BaseURL string `toml:"xyq_api_url"`
		PPEEnv  string `toml:"x-tt-env"`
	}
	metadata, err := toml.DecodeFile(path, &values)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取配置 %s 失败: %w", path, err)
	}
	if unknown := metadata.Undecoded(); len(unknown) > 0 {
		return nil, fmt.Errorf("配置 %s 包含未知字段: %v", path, unknown)
	}
	if baseURL := strings.TrimSpace(values.BaseURL); baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("配置 %s 的 xyq_api_url 必须为不含认证信息、查询参数或 fragment 的 HTTP(S) URL", path)
		}
		cfg.BaseURL = strings.TrimRight(baseURL, "/")
	}
	cfg.PPEEnv = strings.TrimSpace(values.PPEEnv)
	if cfg.PPEEnv != "" && !ppeEnvPattern.MatchString(cfg.PPEEnv) {
		return nil, fmt.Errorf("配置 %s 的 x-tt-env 必须匹配 ppe_[A-Za-z0-9_]+", path)
	}
	return cfg, nil
}

var ppeEnvPattern = regexp.MustCompile(`^ppe_[A-Za-z0-9_]+$`)

type Paths struct {
	SubmitRun             string
	GetCreditBalance      string
	GetAvailableModelList string
	GetThread             string
	UploadFile            string
	ListThreadFile        string
}

// Load resolves the built-in runtime config.
func Load() *Config {
	return &Config{
		BaseURL:     DefaultBaseURL,
		HTTPTimeout: DefaultHTTPTimeout,
		AuthTTL:     DefaultAuthTTL,
		AccessKey:   strings.TrimSpace(os.Getenv(EnvXYQAccessKey)),
		Paths: &Paths{
			SubmitRun:             SubmitRunPath,
			GetCreditBalance:      GetCreditBalancePath,
			GetAvailableModelList: GetAvailableModelListPath,
			GetThread:             GetThreadPath,
			UploadFile:            UploadFilePath,
			ListThreadFile:        ListThreadFilePath,
		},
	}
}
