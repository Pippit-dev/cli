package models

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

const validResponse = `{"ret":"0","data":{"scene":"web_turbo_video_generator","config_key":"key1","config":{"models":[{"key":"new-model","kind":"video","name":"新模型","is_default":true,"supported_ratio_list":[0,1],"audio_total_limit":0,"future_field":9007199254740993}]}}}`

func newTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.Load()
	cfg.BaseURL, cfg.AccessKey = server.URL, "private-test-key"
	runner := common.NewRunner(cfg, nil)
	runner.Auth = auth.NewManager(cfg)
	runner.Client = common.NewHTTPClient(server.URL, time.Second, common.NewAccessKeyContextProviderAuthorizer(runner.Auth.ResolveAccessKey))
	s := NewService(runner)
	s.cacheDir = t.TempDir()
	return s
}

type unavailableAuth struct{ common.AuthManager }

func (unavailableAuth) ResolveAccessKey(context.Context) (string, error) {
	return "", errors.New("expired credential")
}

func TestMissingCredentialsCannotReadFreshCache(t *testing.T) {
	requests := 0
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(validResponse))
	})
	if _, err := s.Get(context.Background(), "video", false); err != nil {
		t.Fatal(err)
	}
	s.runner.Auth = unavailableAuth{}
	result, err := s.Get(context.Background(), "video", false)
	if err == nil || result != nil || requests != 1 || !strings.Contains(err.Error(), "login") {
		t.Fatalf("must require login before cache: result=%+v err=%v requests=%d", result, err, requests)
	}
}

func TestCacheTTLRefreshAndIsolation(t *testing.T) {
	requests := 0
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "POST" || r.URL.Path != config.GetAvailableModelListPath || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["scene"] != VideoScene || body["source_model_key"] != "" {
			t.Errorf("unexpected body: %v, err=%v", body, err)
		}
		_, _ = w.Write([]byte(validResponse))
	})
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	check := func(refresh, cached bool, count int) *Result {
		t.Helper()
		result, err := s.Get(context.Background(), "video", refresh)
		if err != nil || result.Cached != cached || requests != count {
			t.Fatalf("Get: result=%+v err=%v requests=%d, want cached=%v requests=%d", result, err, requests, cached, count)
		}
		return result
	}
	first := check(false, false, 1)
	now = now.Add(CacheTTL - time.Second)
	cached := check(false, true, 1)
	if !cached.FetchedAt.Equal(first.FetchedAt) {
		t.Fatal("cache hit must not renew TTL")
	}
	now = now.Add(time.Second)
	check(false, false, 2) // expires exactly at five minutes
	check(true, false, 3)
	s.runner.Config.AccessKey = "second-account"
	check(false, false, 4)
	oldPath := s.cachePath("second-account", config.GetAvailableModelListPath, VideoScene)
	s.runner.Config.BaseURL += "/other-api"
	if s.cachePath("second-account", config.GetAvailableModelListPath, VideoScene) == oldPath {
		t.Fatal("API base URLs must not share cache")
	}
	files, _ := os.ReadDir(s.cacheDir)
	for _, file := range files {
		raw, _ := os.ReadFile(filepath.Join(s.cacheDir, file.Name()))
		if strings.Contains(string(raw)+file.Name(), "private-test-key") || strings.Contains(string(raw)+file.Name(), "second-account") {
			t.Fatal("cache must not persist raw credentials")
		}
	}
}

func TestFailedQueryDoesNotUseOrCacheStaleData(t *testing.T) {
	response := validResponse
	requests := 0
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(response))
	})
	if _, err := s.Get(context.Background(), "video", false); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"ret":"1001","errmsg":"unavailable","log_id":"log_123"}`,
		`{"ret":"0","data":null}`,
		`{"ret":"0","data":{"scene":"other","config_key":"key","config":{}}}`,
		`not-json`,
	} {
		response = invalid
		if result, err := s.Get(context.Background(), "video", true); err == nil || result != nil || !strings.Contains(err.Error(), "重试") {
			t.Fatalf("failed query must suggest retry: result=%+v err=%v", result, err)
		}
		before := requests
		if _, err := s.Get(context.Background(), "video", false); err == nil || requests != before+1 {
			t.Fatal("retry must query the server, not reuse cached success or failure")
		}
	}
	response = validResponse
	if _, err := s.Get(context.Background(), "video", false); err != nil {
		t.Fatal(err)
	}
	cachePath := s.cachePath(s.runner.Config.AccessKey, config.GetAvailableModelListPath, VideoScene)
	if err := os.WriteFile(cachePath, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := s.Get(context.Background(), "video", false)
	if err != nil || result.Cached {
		t.Fatalf("corrupt cache should refresh: %+v %v", result, err)
	}
	// A timestamp in the future must not extend visibility indefinitely.
	s.now = func() time.Time { return result.FetchedAt.Add(-time.Second) }
	if s.readCache(cachePath, VideoScene) != nil {
		t.Fatal("future-dated cache must be ignored")
	}
}

func TestModelConfigurationPreservedAcrossCache(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(validResponse)) })
	for i := 0; i < 2; i++ {
		result, err := s.Get(context.Background(), "video", false)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := result.Catalog.Describe("new-model")
		if err != nil || !strings.Contains(string(raw), "9007199254740993") || !strings.Contains(string(raw), `"audio_total_limit":0`) {
			t.Fatalf("configuration lost values: %s %v", raw, err)
		}
		var details map[string]json.RawMessage
		if err := json.Unmarshal(raw, &details); err != nil {
			t.Fatal(err)
		}
		if _, exists := details["is_default"]; exists {
			t.Fatal("model detail must not expose the server default marker")
		}
		list, err := json.Marshal(result.Catalog.Search(""))
		if err != nil || strings.Contains(string(list), `"is_default"`) {
			t.Fatalf("model list must not expose the server default marker: %s %v", list, err)
		}
		if !strings.Contains(string(result.Catalog.Config.Models[0]), `"is_default":true`) {
			t.Fatal("presentation must preserve the raw catalog across cache reads")
		}
		if len(result.Catalog.Search("新模")) != 1 || len(result.Catalog.Search("missing")) != 0 {
			t.Fatal("unexpected search result")
		}
		if _, err := result.Catalog.Describe("missing"); err == nil {
			t.Fatal("missing model must not invent a config")
		}
	}
}

func TestCacheWriteFailureStillReturnsServerResult(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(validResponse)) })
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.cacheDir = file
	result, err := s.Get(context.Background(), "video", false)
	if err != nil || result.Cached || result.Warning == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestEmptyAndDuplicateModels(t *testing.T) {
	for _, tc := range []struct {
		data  string
		valid bool
	}{
		{`{"scene":"web_turbo_video_generator","config_key":"empty","config":{}}`, true},
		{`{"scene":"web_turbo_video_generator","config_key":"dup","config":{"models":[{"key":"x","kind":"video"},{"key":"x","kind":"video"}]}}`, false},
		{`{"scene":"web_turbo_video_generator","config_key":"null","config":{"models":[null]}}`, false},
	} {
		var catalog Catalog
		if err := json.Unmarshal([]byte(tc.data), &catalog); err != nil {
			t.Fatal(err)
		}
		if err := validateCatalog(&catalog, VideoScene); (err == nil) != tc.valid {
			t.Fatalf("validateCatalog = %v", err)
		}
	}
}

func TestImageAndVideoCacheIsolation(t *testing.T) {
	requests := map[string]int{}
	failImage := false
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		scene := body["scene"]
		requests[scene]++
		if body["source_model_key"] != "" || len(body) != 2 {
			t.Errorf("unexpected identity or selector fields: %v", body)
		}
		response := validResponse
		switch scene {
		case ImageScene:
			response = strings.ReplaceAll(strings.ReplaceAll(validResponse, VideoScene, ImageScene), `"kind":"video"`, `"kind":"image"`)
			if failImage {
				response = `{"ret":"1001","errmsg":"scene not supported"}`
			}
		case VideoScene:
		default:
			t.Errorf("unexpected scene %q", scene)
		}
		_, _ = w.Write([]byte(response))
	})
	for _, kind := range []string{"image", "video"} {
		for i := 0; i < 2; i++ {
			result, err := s.Get(context.Background(), kind, false)
			if err != nil {
				t.Fatal(err)
			}
			if result.Cached != (i == 1) || result.Catalog.Search("")[0].Kind != kind {
				t.Fatalf("cache crossed model type: %+v", result)
			}
		}
	}
	if requests[ImageScene] != 1 || requests[VideoScene] != 1 {
		t.Fatalf("requests=%v", requests)
	}
	// Even a syntactically valid video cache at the image path must be ignored.
	videoPath := s.cachePath(s.runner.Config.AccessKey, config.GetAvailableModelListPath, VideoScene)
	imagePath := s.cachePath(s.runner.Config.AccessKey, config.GetAvailableModelListPath, ImageScene)
	raw, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := s.Get(context.Background(), "image", false)
	if err != nil || result.Cached || requests[ImageScene] != 2 {
		t.Fatalf("mismatched cache not refreshed: result=%+v err=%v requests=%v", result, err, requests)
	}
	failImage = true
	for _, refresh := range []bool{true, false} {
		if result, err := s.Get(context.Background(), "image", refresh); err == nil || result != nil {
			t.Fatalf("image failure reused a catalog: result=%+v err=%v", result, err)
		}
	}
	result, err = s.Get(context.Background(), "video", false)
	if err != nil || !result.Cached || requests[ImageScene] != 4 || requests[VideoScene] != 1 {
		t.Fatalf("image refresh invalidated video: result=%+v err=%v requests=%v", result, err, requests)
	}
}

func TestImageCatalogValidation(t *testing.T) {
	for _, tc := range []struct {
		name, scene, models string
		valid               bool
	}{
		{"empty", ImageScene, `[]`, true},
		{"image", ImageScene, `[{"key":"dynamic-image","name":"图片测试模型","kind":"image"}]`, true},
		{"missing name", ImageScene, `[{"key":"x","kind":"image"}]`, false},
		{"blank name", ImageScene, `[{"key":"x","kind":"image","name":" "}]`, false},
		{"unknown enum is not a display name", ImageScene, `[{"key":"future_code","name":"future_code","kind":"image"}]`, false},
		{"duplicate name", ImageScene, `[{"key":"x","kind":"image","name":"名称"},{"key":"y","kind":"image","name":" 名称 "}]`, false},
		{"wrong scene", VideoScene, `[{"key":"x","kind":"image"}]`, false},
		{"wrong kind", ImageScene, `[{"key":"x","kind":"video"}]`, false},
		{"duplicate", ImageScene, `[{"key":"x","kind":"image"},{"key":"x","kind":"image"}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"ret":"0","data":{"scene":"` + tc.scene + `","config_key":"key","config":{"models":` + tc.models + `}}}`))
			})
			result, err := s.Get(context.Background(), "image", false)
			if (err == nil) != tc.valid {
				t.Fatalf("result=%+v err=%v valid=%v", result, err, tc.valid)
			}
		})
	}
}

func TestImageNamesKeepWireKeysInternalAcrossCache(t *testing.T) {
	const response = `{"ret":"0","data":{"scene":"web_image_agent","config_key":"image-config","config":{"models":[{"key":"wire-image","name":"智能图片V2","kind":"image"},{"key":"wire-image-fast","report_name":"wire-image-fast","name":"智能图片V2.5 Fast","kind":"image","is_default":true,"supported_ratio_list":[3,6]}]}}}`
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(response)) })
	for i := 0; i < 2; i++ {
		result, err := s.Get(context.Background(), "image", false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Cached != (i == 1) {
			t.Fatalf("cached=%v", result.Cached)
		}
		catalog := result.Catalog
		list := catalog.Search("智能图片")
		if len(list) != 2 || list[1].Name != "智能图片V2.5 Fast" {
			t.Fatalf("unexpected image names: %+v", list)
		}
		if len(catalog.Search("wire-image")) != 0 || len(catalog.Search("fast")) != 1 {
			t.Fatal("image search must use display names only")
		}
		raw, err := json.Marshal(list)
		if err != nil || strings.Contains(string(raw), `"key"`) || strings.Contains(string(raw), "wire-image") {
			t.Fatalf("list leaked identifiers: %s, %v", raw, err)
		}
		detail, err := catalog.Describe(list[1].Name)
		if err != nil || strings.Contains(string(detail), "wire-image") || !strings.Contains(string(detail), "智能图片V2.5 Fast") {
			t.Fatalf("detail leaked identifiers or lost name: %s, %v", detail, err)
		}
		key, err := catalog.ImageModelKey(" 智能图片V2.5 Fast ")
		if err != nil || key != "wire-image-fast" {
			t.Fatalf("name resolution: key=%q, err=%v", key, err)
		}
		for _, invalid := range []string{"", "智能图片", "wire-image-fast", "不存在"} {
			if _, err := catalog.ImageModelKey(invalid); err == nil || strings.Contains(err.Error(), "wire-image") {
				t.Fatalf("unresolved input accepted or leaked: %q, %v", invalid, err)
			}
			if _, err := catalog.Describe(invalid); err == nil || strings.Contains(err.Error(), "wire-image") {
				t.Fatalf("detail accepted unresolved input: %q, %v", invalid, err)
			}
		}
		if !strings.Contains(string(catalog.Config.Models[1]), "wire-image-fast") {
			t.Fatal("presentation mutated the cached request identifier")
		}
		if got := catalog.ImageDisplayMessage("wire-image-fast / wire-image"); got != "智能图片V2.5 Fast / 智能图片V2" {
			t.Fatalf("error must use names: %s", got)
		}
	}
}
