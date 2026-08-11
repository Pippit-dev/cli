package common

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type observedHeaders struct {
	authorization string
	usePPE        string
	ppeEnv        string
	scheduleVDC   string
}

func TestHTTPClientScopesCredentialsAndPPEToBaseOrigin(t *testing.T) {
	apiHeaders := make(chan observedHeaders, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiHeaders <- captureRoutingHeaders(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	thirdPartyHeaders := make(chan observedHeaders, 1)
	thirdParty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		thirdPartyHeaders <- captureRoutingHeaders(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer thirdParty.Close()

	client := NewHTTPClientWithPPEEnv(
		api.URL,
		time.Second,
		NewAccessKeyAuthorizer("secret-ak"),
		func() string { return "ppe_cli_canvas_ak" },
	)
	if err := client.SendRequest(context.Background(), "/api/test", nil, nil); err != nil {
		t.Fatalf("same-origin SendRequest() error = %v", err)
	}
	gotAPI := <-apiHeaders
	if gotAPI.authorization != "Bearer secret-ak" {
		t.Fatalf("same-origin Authorization = %q", gotAPI.authorization)
	}
	if gotAPI.usePPE != "1" || gotAPI.ppeEnv != "ppe_cli_canvas_ak" || gotAPI.scheduleVDC != defaultPPEVDC {
		t.Fatalf("same-origin PPE headers = %#v", gotAPI)
	}

	thirdPartyURL := thirdParty.URL + "/asset.mp4"
	err := client.SendRequestWithHeaders(context.Background(), thirdPartyURL, nil, map[string]string{
		"Authorization":  "Bearer caller-supplied",
		"x-use-ppe":      "1",
		"x-tt-env":       "ppe_leak",
		"x-schedule-vdc": "leak-vdc",
	}, nil)
	if err != nil {
		t.Fatalf("third-party SendRequest() error = %v", err)
	}
	gotThirdParty := <-thirdPartyHeaders
	if gotThirdParty != (observedHeaders{}) {
		t.Fatalf("third-party sensitive headers leaked: %#v", gotThirdParty)
	}
}

func TestHTTPClientTreatsSameOriginAbsoluteURLAsAPI(t *testing.T) {
	headers := make(chan observedHeaders, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- captureRoutingHeaders(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewHTTPClientWithPPEEnv(
		server.URL+"/base-path",
		time.Second,
		NewAccessKeyAuthorizer("same-origin-ak"),
		func() string { return "ppe_absolute" },
	)
	if err := client.SendRequest(context.Background(), server.URL+"/api/absolute", nil, nil); err != nil {
		t.Fatalf("SendRequest() error = %v", err)
	}
	got := <-headers
	if got.authorization != "Bearer same-origin-ak" || got.usePPE != "1" || got.ppeEnv != "ppe_absolute" || got.scheduleVDC != defaultPPEVDC {
		t.Fatalf("same-origin absolute headers = %#v", got)
	}
}

func TestHTTPClientRejectsAPICrossOriginRedirectBeforeSendingBody(t *testing.T) {
	var thirdPartyRequests atomic.Int32
	thirdParty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		thirdPartyRequests.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer thirdParty.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, thirdParty.URL+"/redirected-asset", http.StatusTemporaryRedirect)
	}))
	defer api.Close()

	client := NewHTTPClientWithPPEEnv(
		api.URL,
		time.Second,
		NewAccessKeyAuthorizer("redirect-ak"),
		func() string { return "ppe_redirect" },
	)
	err := client.SendRequest(context.Background(), "/api/redirect", map[string]string{"canvas": "private"}, nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝 Pippit API 跨域重定向") {
		t.Fatalf("SendRequest() error = %v, want cross-origin redirect rejection", err)
	}
	if got := thirdPartyRequests.Load(); got != 0 {
		t.Fatalf("third party received %d API redirect requests, want 0", got)
	}
}

func TestHTTPClientDoesNotRestoreProtectedHeadersAfterCrossOriginRedirect(t *testing.T) {
	finalHeaders := make(chan observedHeaders, 1)
	var apiURL string
	thirdParty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := captureRoutingHeaders(r); got != (observedHeaders{}) {
			t.Errorf("cross-origin hop leaked protected headers: %#v", got)
		}
		http.Redirect(w, r, apiURL+"/final", http.StatusFound)
	}))
	defer thirdParty.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finalHeaders <- captureRoutingHeaders(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()
	apiURL = api.URL

	client := NewHTTPClientWithPPEEnv(
		api.URL,
		time.Second,
		NewAccessKeyAuthorizer("redirect-bounce-ak"),
		func() string { return "ppe_redirect_bounce" },
	)
	if err := client.SendRequest(context.Background(), thirdParty.URL+"/bounce", nil, nil); err != nil {
		t.Fatalf("SendRequest() error = %v", err)
	}
	if got := <-finalHeaders; got != (observedHeaders{}) {
		t.Fatalf("trusted return hop restored protected headers: %#v", got)
	}
}

func TestHTTPClientRejectsInvalidPPEEnvBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewHTTPClientWithPPEEnv(
		server.URL,
		time.Second,
		NewAccessKeyAuthorizer("secret-ak"),
		func() string { return "production" },
	)
	err := client.SendRequest(context.Background(), "/api/test", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "PPE 环境") {
		t.Fatalf("SendRequest() error = %v, want invalid PPE error", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("server received %d requests, want 0", got)
	}
}

func TestHTTPClientProductionRequestOmitsPPEHeaders(t *testing.T) {
	headers := make(chan observedHeaders, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- captureRoutingHeaders(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewHTTPClientWithPPEEnv(
		server.URL,
		time.Second,
		NewAccessKeyAuthorizer("production-ak"),
		func() string { return "" },
	)
	err := client.SendRequestWithHeaders(context.Background(), "/api/test", nil, map[string]string{
		"Authorization":  "Bearer caller-override",
		"x-use-ppe":      "1",
		"x-tt-env":       "ppe_caller_injection",
		"x-schedule-vdc": "caller-vdc",
	}, nil)
	if err != nil {
		t.Fatalf("SendRequest() error = %v", err)
	}
	got := <-headers
	if got.authorization != "Bearer production-ak" || got.usePPE != "" || got.ppeEnv != "" || got.scheduleVDC != "" {
		t.Fatalf("production headers = %#v", got)
	}
}

func TestHTTPClientProtectsConfiguredPPEHeadersFromCallerOverride(t *testing.T) {
	headers := make(chan observedHeaders, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- captureRoutingHeaders(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewHTTPClientWithPPEEnv(
		server.URL,
		time.Second,
		NewAccessKeyAuthorizer("configured-ak"),
		func() string { return "ppe_configured" },
	)
	err := client.SendRequestWithHeaders(context.Background(), "/api/test", nil, map[string]string{
		"Authorization":  "Bearer caller-override",
		"x-use-ppe":      "0",
		"x-tt-env":       "ppe_caller_override",
		"x-schedule-vdc": "caller-vdc",
	}, nil)
	if err != nil {
		t.Fatalf("SendRequest() error = %v", err)
	}
	got := <-headers
	if got.authorization != "Bearer configured-ak" || got.usePPE != "1" || got.ppeEnv != "ppe_configured" || got.scheduleVDC != defaultPPEVDC {
		t.Fatalf("protected headers = %#v", got)
	}
}

func TestHTTPClientMultipartRequestInjectsPPEHeaders(t *testing.T) {
	headers := make(chan observedHeaders, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- captureRoutingHeaders(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	filePath := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(filePath, []byte("sample"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewHTTPClientWithPPEEnv(
		server.URL,
		time.Second,
		NewAccessKeyAuthorizer("upload-ak"),
		func() string { return "ppe_upload" },
	)
	err := client.SendMultipartRequest(context.Background(), "/api/upload", nil, MultipartFile{
		FieldName: "file",
		Path:      filePath,
	}, nil)
	if err != nil {
		t.Fatalf("SendMultipartRequest() error = %v", err)
	}
	got := <-headers
	if got.authorization != "Bearer upload-ak" || got.usePPE != "1" || got.ppeEnv != "ppe_upload" || got.scheduleVDC != defaultPPEVDC {
		t.Fatalf("multipart headers = %#v", got)
	}
}

func TestHTTPClientMultipartRequestStreamsProvidedReader(t *testing.T) {
	contents := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		payload, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		contents <- string(payload)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, time.Second, NewAccessKeyAuthorizer("reader-ak"))
	err := client.SendMultipartRequest(context.Background(), "/api/upload", nil, MultipartFile{
		FieldName: "file",
		FileName:  "verified.bin",
		Reader:    bytes.NewBufferString("verified bytes"),
	}, nil)
	if err != nil {
		t.Fatalf("SendMultipartRequest() error = %v", err)
	}
	if got := <-contents; got != "verified bytes" {
		t.Fatalf("multipart contents = %q", got)
	}
}

func TestSameOriginUsesEffectiveDefaultPorts(t *testing.T) {
	httpsDefault, _ := url.Parse("https://xyq.jianying.com/api")
	httpsExplicit, _ := url.Parse("https://XYQ.JIANYING.COM:443/asset")
	if !sameOrigin(httpsDefault, httpsExplicit) {
		t.Fatal("sameOrigin() = false for equivalent HTTPS origins")
	}

	differentPort, _ := url.Parse("https://xyq.jianying.com:8443/api")
	if sameOrigin(httpsDefault, differentPort) {
		t.Fatal("sameOrigin() = true for different ports")
	}
}

func captureRoutingHeaders(r *http.Request) observedHeaders {
	return observedHeaders{
		authorization: r.Header.Get("Authorization"),
		usePPE:        r.Header.Get(ppeUseHeader),
		ppeEnv:        r.Header.Get(ppeEnvHeader),
		scheduleVDC:   r.Header.Get(ppeScheduleVDCHeader),
	}
}
