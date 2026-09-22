package models

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/version"
)

const (
	VideoScene = "web_turbo_video_generator"
	ImageScene = "web_image_agent"
	CacheTTL   = 5 * time.Minute
)

// Catalog preserves the server's model configuration, including unknown fields
// and integer enums. It is never used as a local generation allowlist.
type Catalog struct {
	Scene     string `json:"scene"`
	ConfigKey string `json:"config_key"`
	Config    *struct {
		Models []json.RawMessage `json:"models"`
	} `json:"config"`
}

type Summary struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type Result struct {
	Catalog   *Catalog
	Cached    bool
	FetchedAt time.Time
	Warning   string
}

type cacheEntry struct {
	FetchedAt time.Time `json:"fetched_at"`
	Catalog   *Catalog  `json:"catalog"`
}

type Service struct {
	runner   *common.Runner
	cacheDir string
	now      func() time.Time
}

func NewService(runner *common.Runner) *Service {
	dir, err := os.UserCacheDir()
	if err == nil {
		dir = filepath.Join(dir, "pippit-cli", "models")
	}
	return &Service{runner: runner, cacheDir: dir, now: time.Now}
}

func sceneForType(modelType string) (string, error) {
	switch modelType {
	case "video":
		return VideoScene, nil
	case "image":
		return ImageScene, nil
	default:
		return "", fmt.Errorf("不支持的模型类型 %q；--type 可选 video 或 image", modelType)
	}
}

func (s *Service) Get(ctx context.Context, modelType string, refresh bool) (*Result, error) {
	scene, err := sceneForType(modelType)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.runner == nil || s.runner.Config == nil || s.runner.Client == nil || s.runner.Auth == nil {
		return nil, fmt.Errorf("模型查询运行器未初始化")
	}
	// Resolve credentials before reading cache: logout/expiry must not expose a
	// previous account's model list. Only a hash is used in the cache filename.
	accessKey, err := s.runner.Auth.ResolveAccessKey(ctx)
	if err != nil || strings.TrimSpace(accessKey) == "" {
		return nil, fmt.Errorf("模型查询需要有效登录，请执行 pippit-tool-cli login 或检查 XYQ_ACCESS_KEY 后重试")
	}
	path := config.GetAvailableModelListPath
	if s.runner.Config.Paths != nil && s.runner.Config.Paths.GetAvailableModelList != "" {
		path = s.runner.Config.Paths.GetAvailableModelList
	}
	cachePath := s.cachePath(accessKey, path, scene)
	if !refresh && cachePath != "" {
		if entry := s.readCache(cachePath, scene); entry != nil {
			return &Result{Catalog: entry.Catalog, Cached: true, FetchedAt: entry.FetchedAt}, nil
		}
	}
	// A failed refresh must not make the next retry reuse an older fresh entry.
	if cachePath != "" {
		_ = os.Remove(cachePath)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var resp struct {
		Ret    string   `json:"ret"`
		Errmsg string   `json:"errmsg"`
		LogID  string   `json:"log_id"`
		Data   *Catalog `json:"data"`
	}
	err = s.runner.Client.SendRequest(queryCtx, path, struct {
		Scene          string `json:"scene"`
		SourceModelKey string `json:"source_model_key"`
	}{Scene: scene}, &resp)
	if err != nil {
		return nil, fmt.Errorf("模型查询失败，请稍后重试（可加 --refresh）: %w", err)
	}
	if resp.Ret != "0" {
		return nil, common.NewLogIDError(fmt.Sprintf("模型查询失败，请稍后重试（可加 --refresh）: ret=%s errmsg=%s", resp.Ret, resp.Errmsg), resp.LogID)
	}
	if err := validateCatalog(resp.Data, scene); err != nil {
		return nil, common.NewLogIDError("模型配置无效，请稍后重试（可加 --refresh）: "+err.Error(), resp.LogID)
	}
	currentKey, err := s.runner.Auth.ResolveAccessKey(ctx)
	if err != nil || currentKey != accessKey {
		return nil, fmt.Errorf("查询期间登录凭证已变化，请重试模型查询")
	}
	result := &Result{Catalog: resp.Data, FetchedAt: s.now().UTC()}
	if cachePath == "" || s.writeCache(cachePath, result) != nil {
		result.Warning = "模型查询成功，但本地缓存写入失败；下次查询将重新请求服务端"
	}
	return result, nil
}

func (s *Service) cachePath(accessKey, path, scene string) string {
	if s.cacheDir == "" {
		return ""
	}
	scope, _ := json.Marshal([]string{
		"v1", strings.TrimRight(s.runner.Config.BaseURL, "/"), path, scene,
		version.Current(), accessKey,
	})
	return filepath.Join(s.cacheDir, fmt.Sprintf("%x.json", sha256.Sum256(scope)))
}

func (s *Service) readCache(path, scene string) *cacheEntry {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entry cacheEntry
	if json.Unmarshal(raw, &entry) != nil || validateCatalog(entry.Catalog, scene) != nil {
		return nil
	}
	age := s.now().Sub(entry.FetchedAt)
	if age < 0 || age >= CacheTTL {
		return nil
	}
	return &entry
}

func (s *Service) writeCache(path string, result *Result) error {
	raw, err := json.Marshal(cacheEntry{FetchedAt: result.FetchedAt, Catalog: result.Catalog})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.cacheDir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.cacheDir, ".models-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func validateCatalog(catalog *Catalog, scene string) error {
	kind := "video"
	if scene == ImageScene {
		kind = "image"
	} else if scene != VideoScene {
		return fmt.Errorf("不支持的模型场景 %q", scene)
	}
	if catalog == nil || catalog.Config == nil || catalog.Scene != scene || catalog.ConfigKey == "" {
		return fmt.Errorf("缺少配置或场景不匹配")
	}
	seen := make(map[string]bool)
	for _, raw := range catalog.Config.Models {
		var model Summary
		if json.Unmarshal(raw, &model) != nil || strings.TrimSpace(model.Key) == "" || model.Kind != kind {
			return fmt.Errorf("模型条目缺少有效 key 或 kind")
		}
		if seen[model.Key] {
			return fmt.Errorf("模型 key 重复: %s", model.Key)
		}
		seen[model.Key] = true
	}
	return nil
}

func (c *Catalog) Search(query string) []Summary {
	query = strings.TrimSpace(query)
	matches := make([]Summary, 0)
	for _, raw := range c.Config.Models {
		var model Summary
		_ = json.Unmarshal(raw, &model) // validated at the network/cache boundary
		if model.Key == query {
			return []Summary{model}
		}
		if query == "" || strings.Contains(strings.ToLower(model.Name), strings.ToLower(query)) || strings.Contains(strings.ToLower(model.Key), strings.ToLower(query)) {
			matches = append(matches, model)
		}
	}
	return matches
}

func (c *Catalog) Describe(key string) (json.RawMessage, error) {
	key = strings.TrimSpace(key)
	for _, raw := range c.Config.Models {
		var model Summary
		_ = json.Unmarshal(raw, &model)
		if model.Key == key {
			return describeModel(raw)
		}
	}
	kind := "video"
	if c.Scene == ImageScene {
		kind = "image"
	}
	return nil, fmt.Errorf("未找到可用模型 %q；请执行 model list --type %s --refresh 查看当前模型", key, kind)
}
