package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/version"
)

func newOriginTestFlow(t *testing.T) (*browserFlow, accessKeyPayload) {
	t.Helper()
	authURL, _ := url.Parse(config.DefaultBaseURL)
	deviceID, _ := randomEncoded(bytes.NewReader(bytes.Repeat([]byte{0x21}, deviceIDBytes)), deviceIDBytes)
	flow, err := startBrowserFlow(authURL, bytes.NewReader(bytes.Repeat([]byte{0x42}, 2*randomBindingBytes)), deviceID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(flow.close)
	return flow, accessKeyPayload{
		Type: "access_key", AccessKey: "local-test-ak", UID: "test-user", TokenID: "test-token",
		ExpiredAt: time.Now().Add(time.Hour).Unix(), RandomSecretKey: flow.secret,
		Source: loginSource, CallbackURL: flow.callbackURL,
	}
}

func originTestRequest(t *testing.T, flow *browserFlow, method, origin string, payload accessKeyPayload, mutations ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, flow.callbackURL, bytes.NewReader(body))
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "Content-Type")
	for _, mutate := range mutations {
		mutate(request)
	}
	response := httptest.NewRecorder()
	flow.handleCallback(response, request)
	return response
}

func TestBrowserCallbackAcceptsBothOfficialOriginsWithMatchingCORS(t *testing.T) {
	for _, origin := range []string{"https://xyq.jianying.com", "https://www.xiaoyunque.com"} {
		t.Run(origin, func(t *testing.T) {
			flow, payload := newOriginTestFlow(t)
			for _, method := range []string{http.MethodOptions, http.MethodPost} {
				response := originTestRequest(t, flow, method, origin, payload)
				wantStatus := http.StatusOK
				if method == http.MethodOptions {
					wantStatus = http.StatusNoContent
				}
				if response.Code != wantStatus || response.Header().Get("Access-Control-Allow-Origin") != origin {
					t.Fatalf("%s status/CORS = %d/%q", method, response.Code, response.Header().Get("Access-Control-Allow-Origin"))
				}
				if response.Header().Get("Access-Control-Allow-Private-Network") != "true" ||
					!strings.Contains(strings.Join(response.Header().Values("Vary"), ","), "Origin") {
					t.Fatal("callback lost private-network or Vary headers")
				}
			}
			// Switching between the two trusted sites must not reset the one-shot binding.
			otherOrigin := "https://xyq.jianying.com"
			if origin == otherOrigin {
				otherOrigin = "https://www.xiaoyunque.com"
			}
			if response := originTestRequest(t, flow, http.MethodPost, otherOrigin, payload); response.Code != http.StatusConflict {
				t.Fatalf("cross-origin replay status = %d", response.Code)
			}
		})
	}
}

func TestBrowserCallbackRejectsUntrustedOriginsBeforeCORS(t *testing.T) {
	for _, origin := range []string{
		"", "null", "http://xyq.jianying.com", "http://www.xiaoyunque.com",
		"https://www.xiaoyunque.com:443", "https://www.xiaoyunque.com/",
		"https://www.xiaoyunque.com.evil.example", "https://evil.example",
		"https://other.xiaoyunque.com", "https://www.xiaoyunque.com https://xyq.jianying.com",
	} {
		t.Run(origin, func(t *testing.T) {
			flow, payload := newOriginTestFlow(t)
			for _, method := range []string{http.MethodOptions, http.MethodPost} {
				response := originTestRequest(t, flow, method, origin, payload)
				if response.Code != http.StatusForbidden || response.Header().Get("Access-Control-Allow-Origin") != "" {
					t.Fatalf("%s untrusted origin status/CORS = %d/%q", method, response.Code, response.Header().Get("Access-Control-Allow-Origin"))
				}
			}
			if len(flow.payload) != 0 {
				t.Fatal("untrusted origin delivered a credential")
			}
		})
	}
}

func TestNewAuthOriginStillRequiresEveryCallbackBinding(t *testing.T) {
	for _, binding := range []string{"secret", "source", "callback_url", "state", "host"} {
		t.Run(binding, func(t *testing.T) {
			flow, payload := newOriginTestFlow(t)
			switch binding {
			case "secret":
				payload.RandomSecretKey = "wrong"
			case "source":
				payload.Source = "wrong"
			case "callback_url":
				payload.CallbackURL += "&extra=1"
			case "state":
				flow.state = "wrong"
			}
			response := originTestRequest(t, flow, http.MethodPost, "https://www.xiaoyunque.com", payload, func(request *http.Request) {
				if binding == "host" {
					request.Host = "evil.example"
				}
			})
			if response.Code != http.StatusBadRequest || len(flow.payload) != 0 {
				t.Fatalf("invalid %s status = %d, queued callbacks = %d", binding, response.Code, len(flow.payload))
			}
		})
	}
}

func TestBrowserLoginMetadataUsesBinaryVersionAndProtocol(t *testing.T) {
	previous := version.Version
	t.Cleanup(func() { version.Version = previous })
	for _, buildVersion := range []string{"1.0.42", "1.0.42-canary.1", "dev", ""} {
		t.Run(buildVersion, func(t *testing.T) {
			version.Version = buildVersion
			flow, _ := newOriginTestFlow(t)
			loginURL, err := url.Parse(flow.loginURL)
			if err != nil {
				t.Fatal(err)
			}
			wantVersion := buildVersion
			if wantVersion == "" {
				wantVersion = "dev"
			}
			if query := loginURL.Query(); query.Get("cli_version") != wantVersion || query.Get("auth_protocol") != "1" {
				t.Fatalf("auth metadata = version %q, protocol %q", query.Get("cli_version"), query.Get("auth_protocol"))
			}
		})
	}
}
