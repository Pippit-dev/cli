package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type fakeImportAuthAPI struct {
	accessKey       string
	loginKey        string
	probeErrors     []error
	probes          int
	logins          int
	loginErr        error
	resolveErr      error
	explicit        bool
	credentialScope string
	loginOptions    []importAuthLoginOptions
}

type credentialErrorAuthManager struct{ err error }

func (manager credentialErrorAuthManager) ResolveAccessKey(context.Context) (string, error) {
	return "", manager.err
}

func (credentialErrorAuthManager) Login(context.Context, internal_auth.LoginOptions) (*internal_auth.Credential, error) {
	return nil, errors.New("unexpected login")
}

func (credentialErrorAuthManager) Status(context.Context) (*internal_auth.Status, error) {
	return nil, errors.New("unexpected status")
}

func (credentialErrorAuthManager) Logout(context.Context, bool) error {
	return errors.New("unexpected logout")
}

func (credentialErrorAuthManager) CredentialScope(context.Context) (string, error) {
	return "", errors.New("unexpected scope")
}

func (api *fakeImportAuthAPI) AccessKey(context.Context) (string, error) {
	return api.accessKey, api.resolveErr
}

func (api *fakeImportAuthAPI) Login(_ context.Context, _ io.Writer, options importAuthLoginOptions) error {
	api.logins++
	api.loginOptions = append(api.loginOptions, options)
	if api.loginErr != nil {
		return api.loginErr
	}
	if strings.TrimSpace(api.loginKey) == "" {
		api.loginKey = "browser-managed-key"
	}
	api.accessKey = api.loginKey
	api.resolveErr = nil
	return nil
}

func (api *fakeImportAuthAPI) HasExplicitAccessKey() bool { return api.explicit }

func (api *fakeImportAuthAPI) CredentialScope(context.Context) (string, error) {
	if api.credentialScope == "" {
		return "browser-device-scope", nil
	}
	return api.credentialScope, nil
}

func (api *fakeImportAuthAPI) Probe(context.Context) error {
	api.probes++
	if len(api.probeErrors) == 0 {
		return nil
	}
	err := api.probeErrors[0]
	api.probeErrors = api.probeErrors[1:]
	return err
}

func TestEnsureCanvasImportPippitAuthAcceptsExistingKey(t *testing.T) {
	auth := &fakeImportAuthAPI{accessKey: "configured-key"}
	prompted := false

	err := ensureCanvasImportPippitAuth(
		context.Background(),
		auth,
		true,
		func(context.Context, importAuthPromptRequest) (importAuthPromptResponse, error) {
			prompted = true
			return importAuthPromptResponse{}, nil
		},
	)
	if err != nil {
		t.Fatalf("ensureCanvasImportPippitAuth() error = %v", err)
	}
	if auth.probes != 1 || prompted || auth.logins != 0 {
		t.Fatalf("probes/prompted/logins = %d/%v/%d, want 1/false/0", auth.probes, prompted, auth.logins)
	}
}

func TestEnsureCanvasImportPippitAuthMissingKeyIsSideEffectFreeWhenNonInteractive(t *testing.T) {
	auth := &fakeImportAuthAPI{}

	err := ensureCanvasImportPippitAuth(context.Background(), auth, false, nil)
	if err == nil || !strings.Contains(err.Error(), "pippit-tool-cli login") {
		t.Fatalf("ensureCanvasImportPippitAuth() error = %v, want missing-key guidance", err)
	}
	if auth.probes != 0 || auth.logins != 0 {
		t.Fatalf("probes/logins = %d/%d, want no side effects", auth.probes, auth.logins)
	}
}

func TestEnsureCanvasImportPippitAuthMissingCredentialStartsBrowserLoginBeforeProbe(t *testing.T) {
	auth := &fakeImportAuthAPI{loginKey: "browser-key"}
	prompted := false

	err := ensureCanvasImportPippitAuth(
		context.Background(),
		auth,
		true,
		func(context.Context, importAuthPromptRequest) (importAuthPromptResponse, error) {
			prompted = true
			return importAuthPromptResponse{}, nil
		},
	)
	if err != nil {
		t.Fatalf("ensureCanvasImportPippitAuth() error = %v", err)
	}
	if auth.logins != 1 || auth.probes != 1 || prompted {
		t.Fatalf("logins/probes/prompted = %d/%d/%v, want 1/1/false", auth.logins, auth.probes, prompted)
	}
}

func TestEnsureCanvasImportPippitAuthRetriesThenUsesBrowserLogin(t *testing.T) {
	auth := &fakeImportAuthAPI{
		accessKey: "invalid-secret-key",
		probeErrors: []error{
			errors.New("HTTP 401 invalid-secret-key"),
			errors.New("网络暂时不可用"),
			nil,
		},
	}
	responses := []importAuthPromptResponse{
		{Action: importAuthPromptRetry},
		{Action: importAuthPromptLogin},
	}
	requests := make([]importAuthPromptRequest, 0, len(responses))

	err := ensureCanvasImportPippitAuth(
		context.Background(),
		auth,
		true,
		func(_ context.Context, request importAuthPromptRequest) (importAuthPromptResponse, error) {
			requests = append(requests, request)
			response := responses[0]
			responses = responses[1:]
			return response, nil
		},
	)
	if err != nil {
		t.Fatalf("ensureCanvasImportPippitAuth() error = %v", err)
	}
	if auth.probes != 3 || auth.logins != 1 {
		t.Fatalf("probes/logins = %d/%d, want retry then browser login", auth.probes, auth.logins)
	}
	if len(auth.loginOptions) != 1 || !auth.loginOptions[0].ForceRefresh {
		t.Fatalf("login options = %#v, want forced replacement after rejected AK", auth.loginOptions)
	}
	if len(requests) != 2 || !requests[0].HasCredential || !requests[1].HasCredential {
		t.Fatalf("prompt requests = %#v, want failed-key prompts", requests)
	}
	if strings.Contains(requests[0].Failure, "invalid-secret-key") || !strings.Contains(requests[0].Failure, "HTTP 401") {
		t.Fatalf("first prompt failure is not safe, actionable Chinese guidance: %q", requests[0].Failure)
	}
}

func TestEnsureCanvasImportPippitAuthCanCancelAfterProbeFailure(t *testing.T) {
	auth := &fakeImportAuthAPI{
		accessKey:   "configured-key",
		probeErrors: []error{errors.New("HTTP 401")},
	}

	err := ensureCanvasImportPippitAuth(
		context.Background(),
		auth,
		true,
		func(context.Context, importAuthPromptRequest) (importAuthPromptResponse, error) {
			return importAuthPromptResponse{Action: importAuthPromptCancel}, nil
		},
	)
	if !errors.Is(err, errCanvasImportAuthCanceled) {
		t.Fatalf("ensureCanvasImportPippitAuth() error = %v, want cancellation", err)
	}
	if auth.probes != 1 || auth.logins != 0 {
		t.Fatalf("probes/logins = %d/%d, want one read-only probe and no login", auth.probes, auth.logins)
	}
}

func TestEnsureCanvasImportPippitAuthDoesNotExposePromptErrorText(t *testing.T) {
	const candidate = "candidate-key-from-secret-input"

	err := ensureCanvasImportPippitAuth(
		context.Background(),
		&fakeImportAuthAPI{accessKey: "invalid-key", probeErrors: []error{errors.New("HTTP 401")}},
		true,
		func(context.Context, importAuthPromptRequest) (importAuthPromptResponse, error) {
			return importAuthPromptResponse{}, errors.New("failed after reading " + candidate)
		},
	)
	if err == nil || !strings.Contains(err.Error(), "读取小云雀授权选择失败") {
		t.Fatalf("ensureCanvasImportPippitAuth() error = %v, want safe prompt failure", err)
	}
	if strings.Contains(err.Error(), candidate) {
		t.Fatalf("ensureCanvasImportPippitAuth() leaked prompt input: %v", err)
	}
}

func TestEnsureCanvasImportPippitAuthRedactsNonInteractiveProbeFailure(t *testing.T) {
	const accessKey = "do-not-print-this-key"
	auth := &fakeImportAuthAPI{
		accessKey:   accessKey,
		probeErrors: []error{errors.New("rejected do-not-print-this-key")},
	}

	err := ensureCanvasImportPippitAuth(context.Background(), auth, false, nil)
	if err == nil || !strings.Contains(err.Error(), "Access Key 校验失败") {
		t.Fatalf("ensureCanvasImportPippitAuth() error = %v, want validation failure", err)
	}
	if strings.Contains(err.Error(), accessKey) {
		t.Fatalf("ensureCanvasImportPippitAuth() leaked Access Key: %v", err)
	}
}

func TestRunnerImportAuthAPIUsesReadOnlyCanvasQueryAndMemoryOnlyKey(t *testing.T) {
	var method string
	var assetIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		method = request.Method
		if got := request.Header.Get("Authorization"); got != "Bearer pasted-key" {
			t.Errorf("Authorization = %q, want updated in-memory key", got)
		}
		var body struct {
			PippitAssetIDs []string `json:"pippit_asset_ids"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("Decode() error = %v", err)
		}
		assetIDs = body.PippitAssetIDs
		writer.Header().Set("Content-Type", "application/json")
		// The real query endpoint returns an empty data object for a missing
		// sentinel ID, so authorization probing must not require data.Assets.
		_, _ = writer.Write([]byte(`{"ret":"0","log_id":"probe-log","data":{}}`))
	}))
	defer server.Close()

	cfg := &config.Config{BaseURL: server.URL, HTTPTimeout: time.Second, AccessKey: "pasted-key"}
	runner := common.NewRunner(cfg, nil)
	runner.Client = common.NewHTTPClient(
		cfg.BaseURL,
		cfg.HTTPTimeout,
		common.NewAccessKeyProviderAuthorizer(func() string { return runner.Config.AccessKey }),
	)
	auth := runnerImportAuthAPI{runner: runner}

	if err := auth.Probe(context.Background()); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if method != http.MethodPost || strings.Join(assetIDs, ",") != "9223372036854775807" {
		t.Fatalf("probe method/assets = %q/%v, want read-only Canvas query sentinel 9223372036854775807", method, assetIDs)
	}
}

func TestRunnerImportMediaPreflightPreservesCredentialState(t *testing.T) {
	for _, cause := range []error{internal_auth.ErrCredentialNotFound, internal_auth.ErrCredentialExpired} {
		api := runnerImportMediaAPI{runner: &common.Runner{Auth: credentialErrorAuthManager{err: cause}}}
		err := api.PreflightUpload(context.Background())
		if !errors.Is(err, cause) {
			t.Fatalf("PreflightUpload() error = %v, want wrapped %v", err, cause)
		}
	}
}

func TestEnsureCanvasImportPippitAuthHonorsCanceledContextBeforePrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prompted := false

	err := ensureCanvasImportPippitAuth(
		ctx,
		&fakeImportAuthAPI{},
		true,
		func(context.Context, importAuthPromptRequest) (importAuthPromptResponse, error) {
			prompted = true
			return importAuthPromptResponse{}, nil
		},
	)
	if !errors.Is(err, context.Canceled) || prompted {
		t.Fatalf("error/prompted = %v/%v, want canceled before prompt", err, prompted)
	}
}

func TestRedactCanvasImportFinalErrorRemovesCurrentAccessKey(t *testing.T) {
	const accessKey = "current-secret-access-key"
	auth := &fakeImportAuthAPI{accessKey: accessKey}
	err := redactCanvasImportFinalError(
		fmt.Errorf("远端失败，响应包含 %s", accessKey),
		auth,
	)
	if err == nil || strings.Contains(err.Error(), accessKey) || !strings.Contains(err.Error(), "[已隐藏]") {
		t.Fatalf("redacted error = %v, want hidden current key", err)
	}
}

func TestCanvasImportPippitAuthFailureRecognizesBusinessRetCode(t *testing.T) {
	for _, message := range []string{`query failed: ret=1015`, `query failed: ret="1015"`} {
		if !isCanvasImportPippitAuthFailure(errors.New(message)) {
			t.Fatalf("isCanvasImportPippitAuthFailure(%q) = false, want true", message)
		}
	}
}

func TestCanvasImportPippitAuthFailureRecognizesWrappedCredentialState(t *testing.T) {
	for _, cause := range []error{internal_auth.ErrCredentialNotFound, internal_auth.ErrCredentialExpired} {
		err := fmt.Errorf("upload preflight: %w", cause)
		if !isCanvasImportPippitAuthFailure(err) {
			t.Fatalf("wrapped credential error was not recognized: %v", err)
		}
	}
}
