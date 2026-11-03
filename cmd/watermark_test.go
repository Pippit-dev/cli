package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

func TestWatermarkWriteRejectsRedirects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PIPPIT_CLI_DISABLE_UPDATE_CHECK", "1")
	for _, action := range []string{"on", "off"} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			t.Run(fmt.Sprintf("%s/%d", action, status), func(t *testing.T) {
				requests := make(chan string, 2)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests <- r.Method + ":" + r.URL.Path
					if r.URL.Path == config.UpdateWatermarkSettingPath {
						w.Header().Set("Location", "/redirected")
						w.WriteHeader(status)
						return
					}
					fmt.Fprint(w, `{"ret":"0","data":{"enabled":false}}`)
				}))
				defer server.Close()
				cfg := config.Load()
				cfg.BaseURL, cfg.AccessKey = server.URL, "test-key"
				policy := adapter.Default{}
				runner := newRootRunnerWithAdapter(cfg, policy)
				var output bytes.Buffer
				root := newRootCommandWithAdapter(&output, io.Discard, runner, policy)
				root.SetArgs([]string{"watermark", action})
				if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "重定向") {
					t.Fatalf("redirected write error = %v", err)
				}
				if len(requests) != 1 || <-requests != http.MethodPost+":"+config.UpdateWatermarkSettingPath {
					t.Fatal("watermark write followed a redirect or did not issue exactly one POST")
				}
				if output.Len() != 0 {
					t.Fatalf("redirected write reported success: %s", output.String())
				}
			})
		}
	}
}

func TestWatermarkQueryAndWriteUseServerReadback(t *testing.T) {
	t.Setenv("PIPPIT_CLI_DISABLE_UPDATE_CHECK", "1")
	for _, tc := range []struct {
		name    string
		args    []string
		path    string
		body    string
		enabled bool
	}{
		{"query false", nil, config.GetWatermarkSettingPath, `{}`, false},
		{"on", []string{"on"}, config.UpdateWatermarkSettingPath, `{"enabled":true}`, true},
		{"off", []string{"off"}, config.UpdateWatermarkSettingPath, `{"enabled":false}`, false},
		{"server readback differs", []string{"on"}, config.UpdateWatermarkSettingPath, `{"enabled":true}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Method + ":" + r.URL.Path
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != tc.body {
					t.Errorf("request body = %s, error = %v, want %s", body, err, tc.body)
				}
				fmt.Fprintf(w, `{"ret":"0","data":{"enabled":%t}}`, tc.enabled)
			}))
			defer server.Close()
			cfg := config.Load()
			cfg.BaseURL, cfg.AccessKey = server.URL, "test-key"
			policy := adapter.Default{}
			runner := newRootRunnerWithAdapter(cfg, policy)
			var output bytes.Buffer
			root := newRootCommandWithAdapter(&output, io.Discard, runner, policy)
			root.SetArgs(append([]string{"watermark"}, tc.args...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 || <-requests != http.MethodPost+":"+tc.path {
				t.Fatal("watermark used an unexpected endpoint or sent an extra request")
			}
			var result struct {
				Enabled *bool `json:"enabled"`
			}
			if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.Enabled == nil || *result.Enabled != tc.enabled {
				t.Fatalf("output = %s, error = %v", output.String(), err)
			}
		})
	}
}

func TestWatermarkWritePreservesAuthenticationAndPPEWithoutSpace(t *testing.T) {
	t.Setenv("PIPPIT_CLI_DISABLE_UPDATE_CHECK", "1")
	for _, name := range []string{"browser AK", "adapter"} {
		t.Run(name, func(t *testing.T) {
			headers := make(chan http.Header, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != config.UpdateWatermarkSettingPath {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				headers <- r.Header.Clone()
				fmt.Fprint(w, `{"ret":"0","data":{"enabled":false}}`)
			}))
			defer server.Close()
			cfg := config.Load()
			cfg.BaseURL, cfg.AccessKey, cfg.PPEEnv = server.URL, "", "ppe_fixture"
			var policy adapter.Adapter = adapter.Default{}
			if name == "adapter" {
				policy = marketingTestAdapter{}
			}
			runner := newRootRunnerWithAdapter(cfg, policy)
			if name == "browser AK" {
				store := &marketingCredentialStore{credential: &auth.Credential{AccessKey: "browser-secret", UID: "user", DeviceID: "device", ExpiredAt: time.Now().Add(time.Hour).Unix()}}
				runner.Auth = auth.NewManager(cfg, auth.WithCredentialStore(store))
			}
			// Account metadata is unavailable, but this account-level command must still work.
			runner.Auth = spaceUnavailableAccount{AuthManager: runner.Auth}
			root := newRootCommandWithAdapter(io.Discard, io.Discard, runner, policy)
			root.SetArgs([]string{"watermark", "off", "--team-id", "42"})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if len(headers) != 1 {
				t.Fatalf("request count = %d, want 1", len(headers))
			}
			got := <-headers
			if name == "browser AK" && got.Get("Authorization") != "Bearer browser-secret" {
				t.Error("watermark write did not reuse browser login")
			}
			if name == "adapter" && (got.Get("X-Test-Adapter-Auth") != "present" || got.Get("X-Test-Adapter-Transport") != "present") {
				t.Error("watermark write bypassed the root adapter")
			}
			if got.Get("x-tt-env") != "ppe_fixture" || got.Get("x-use-ppe") != "1" || got.Get("X-Team-Id") != "" {
				t.Error("watermark write lost PPE routing or became team-scoped")
			}
		})
	}
}

func TestWatermarkWriteDoesNotChangeQueryRedirects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PIPPIT_CLI_DISABLE_UPDATE_CHECK", "1")
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + ":" + r.URL.Path
		if r.URL.Path != "/redirected" {
			w.Header().Set("Location", "/redirected")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		fmt.Fprint(w, `{"ret":"0","data":{"enabled":false}}`)
	}))
	defer server.Close()
	cfg := config.Load()
	cfg.BaseURL, cfg.AccessKey = server.URL, "test-key"
	policy := adapter.Default{}
	runner := newRootRunnerWithAdapter(cfg, policy)
	write := newRootCommandWithAdapter(io.Discard, io.Discard, runner, policy)
	write.SetArgs([]string{"watermark", "off"})
	if err := write.Execute(); err == nil {
		t.Fatal("redirected write succeeded")
	}
	var output bytes.Buffer
	query := newRootCommandWithAdapter(&output, io.Discard, runner, policy)
	query.SetArgs([]string{"watermark"})
	if err := query.Execute(); err != nil {
		t.Fatalf("query lost its existing redirect behavior: %v", err)
	}
	if len(requests) != 3 {
		t.Fatalf("request count = %d, want one write and two query requests", len(requests))
	}
	for _, path := range []string{config.UpdateWatermarkSettingPath, config.GetWatermarkSettingPath, "/redirected"} {
		if got := <-requests; got != http.MethodPost+":"+path {
			t.Fatalf("request = %s, want POST:%s", got, path)
		}
	}
	if strings.TrimSpace(output.String()) != `{"enabled":false}` {
		t.Fatalf("redirected query output = %s", output.String())
	}
}
