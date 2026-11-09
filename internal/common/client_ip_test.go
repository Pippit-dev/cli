package common

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/space"
)

type clientIPPolicy struct {
	adapter.Default
	prepareCalls int
}

func (p *clientIPPolicy) PrepareAPIRequest(_ context.Context, req *http.Request) (adapter.AuthDecision, error) {
	p.prepareCalls++
	req.Header.Set("Authorization", "Bearer adapter-key")
	req.Header.Set("Cookie", "sessionid=adapter-cookie")
	req.Header.Set("X-Team-Id", "99")
	return adapter.AuthProvided, nil
}

func (*clientIPPolicy) WrapTransport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return clientIPTransport{next: next}
}

type clientIPTransport struct{ next http.RoundTripper }

func (transport clientIPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	request := req.Clone(req.Context())
	request.Header.Set("X-Test-IP-Transport", "present")
	return transport.next.RoundTrip(request)
}

func TestGetClientIPIsAnonymousAndPreservesTransportAndRouting(t *testing.T) {
	for _, env := range []string{"", "ppe_first", "ppe_second"} {
		t.Run("env="+env, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/biz/v1/skill/get_client_ip" {
					t.Errorf("request = %s %s, want anonymous client IP GET", r.Method, r.URL.Path)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || len(body) != 0 || r.Header.Get("Content-Type") != "" {
					t.Errorf("GET body = %q, Content-Type = %q, error = %v", body, r.Header.Get("Content-Type"), err)
				}
				for _, header := range []string{"Authorization", "Cookie", "X-Team-Id"} {
					if r.Header.Get(header) != "" {
						t.Errorf("anonymous request retained %s", header)
					}
				}
				if r.Header.Get("X-Test-IP-Transport") != "present" {
					t.Error("anonymous request bypassed adapter transport")
				}
				wantUsePPE := ""
				if env != "" {
					wantUsePPE = "1"
				}
				if r.Header.Get("x-tt-env") != env || r.Header.Get("x-use-ppe") != wantUsePPE {
					t.Error("anonymous request did not follow the configured PPE environment")
				}
				_, _ = io.WriteString(w, `{"ret":"0","data":{"client_ip":"8.8.8.8"}}`)
			}))
			defer server.Close()

			cfg := config.Load()
			cfg.BaseURL, cfg.PPEEnv, cfg.AccessKey = server.URL, env, "existing-key"
			cfg.HTTPTimeout = time.Second
			policy := &clientIPPolicy{}
			runner := NewRunner(cfg, NewHTTPClientWithAdapter(server.URL, time.Second, NewAccessKeyAuthorizer(cfg.AccessKey), policy))
			teamID := "42"
			ctx := WithSpace(context.Background(), &space.Session{Explicit: &teamID})
			result, err := GetClientIP(ctx, runner, policy)
			if err != nil || result == nil || result.ClientIP != "8.8.8.8" {
				t.Fatalf("GetClientIP() = %#v, %v", result, err)
			}
			if policy.prepareCalls != 0 {
				t.Fatalf("adapter authentication calls = %d, want 0", policy.prepareCalls)
			}
		})
	}
}

func TestGetClientIPRejectsUnusableResponses(t *testing.T) {
	for _, tc := range []struct {
		name, response, wantError string
		status                    int
	}{
		{"unavailable_source", `{"ret":"53001","errmsg":"暂时无法识别当前来源 IP","log_id":"log_ip"}`, "53001", http.StatusOK},
		{"unavailable_source_http503", `{"ret":"53001","errmsg":"暂时无法识别当前来源 IP","log_id":"log_ip"}`, "53001", http.StatusServiceUnavailable},
		{"missing_data", `{"ret":"0","log_id":"log_ip"}`, "data", http.StatusOK},
		{"missing_ip", `{"ret":"0","log_id":"log_ip","data":{}}`, "client_ip", http.StatusOK},
		{"invalid_ip", `{"ret":"0","log_id":"log_ip","data":{"client_ip":"not-an-ip"}}`, "client_ip", http.StatusOK},
		{"zoned_ip", `{"ret":"0","log_id":"log_ip","data":{"client_ip":"fe80::1%eth0"}}`, "client_ip", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			cfg := config.Load()
			cfg.BaseURL = server.URL
			cfg.HTTPTimeout = time.Second
			_, err := GetClientIP(context.Background(), NewRunner(cfg, nil), adapter.Default{})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) || !strings.Contains(err.Error(), "log_ip") {
				t.Fatalf("GetClientIP() error = %v, want %q and log ID", err, tc.wantError)
			}
		})
	}
}

func TestGetClientIPSupportsIPv6(t *testing.T) {
	const ip = "2001:4860:4860::8888"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ret":"0","data":{"client_ip":"`+ip+`"}}`)
	}))
	defer server.Close()
	cfg := config.Load()
	cfg.BaseURL = server.URL
	cfg.HTTPTimeout = time.Second
	result, err := GetClientIP(context.Background(), NewRunner(cfg, nil), adapter.Default{})
	if err != nil || result == nil || result.ClientIP != ip {
		t.Fatalf("GetClientIP() = %#v, %v, want IPv6 %s", result, err, ip)
	}
}
