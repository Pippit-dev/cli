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
	if _, err := s.Get(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	s.runner.Auth = unavailableAuth{}
	result, err := s.Get(context.Background(), false)
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
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["scene"] != Scene || body["source_model_key"] != "" {
			t.Errorf("unexpected body: %v, err=%v", body, err)
		}
		_, _ = w.Write([]byte(validResponse))
	})
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	check := func(refresh, cached bool, count int) *Result {
		t.Helper()
		result, err := s.Get(context.Background(), refresh)
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
	oldPath := s.cachePath("second-account", config.GetAvailableModelListPath)
	s.runner.Config.BaseURL += "/other-api"
	if s.cachePath("second-account", config.GetAvailableModelListPath) == oldPath {
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
	if _, err := s.Get(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"ret":"1001","errmsg":"unavailable","log_id":"log_123"}`,
		`{"ret":"0","data":null}`,
		`{"ret":"0","data":{"scene":"other","config_key":"key","config":{}}}`,
		`not-json`,
	} {
		response = invalid
		if result, err := s.Get(context.Background(), true); err == nil || result != nil || !strings.Contains(err.Error(), "重试") {
			t.Fatalf("failed query must suggest retry: result=%+v err=%v", result, err)
		}
		before := requests
		if _, err := s.Get(context.Background(), false); err == nil || requests != before+1 {
			t.Fatal("retry must query the server, not reuse cached success or failure")
		}
	}
	response = validResponse
	if _, err := s.Get(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	cachePath := s.cachePath(s.runner.Config.AccessKey, config.GetAvailableModelListPath)
	if err := os.WriteFile(cachePath, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := s.Get(context.Background(), false)
	if err != nil || result.Cached {
		t.Fatalf("corrupt cache should refresh: %+v %v", result, err)
	}
	// A timestamp in the future must not extend visibility indefinitely.
	s.now = func() time.Time { return result.FetchedAt.Add(-time.Second) }
	if s.readCache(cachePath) != nil {
		t.Fatal("future-dated cache must be ignored")
	}
}

func TestModelConfigurationPreservedAcrossCache(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(validResponse)) })
	for i := 0; i < 2; i++ {
		result, err := s.Get(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := result.Catalog.Describe("new-model")
		if err != nil || !strings.Contains(string(raw), "9007199254740993") || !strings.Contains(string(raw), `"audio_total_limit":0`) {
			t.Fatalf("configuration lost values: %s %v", raw, err)
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
	result, err := s.Get(context.Background(), false)
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
		if err := validateCatalog(&catalog); (err == nil) != tc.valid {
			t.Fatalf("validateCatalog = %v", err)
		}
	}
}
