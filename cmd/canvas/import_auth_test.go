package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type fakeImportAuthAPI struct {
	accessKey   string
	setValues   []string
	probeErrors []error
	probes      int
	setErr      error
}

func (api *fakeImportAuthAPI) AccessKey() string {
	return api.accessKey
}

func (api *fakeImportAuthAPI) SetAccessKey(accessKey string) error {
	api.setValues = append(api.setValues, accessKey)
	if api.setErr != nil {
		return api.setErr
	}
	api.accessKey = accessKey
	return nil
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
	if auth.probes != 1 || prompted || len(auth.setValues) != 0 {
		t.Fatalf("probes/prompted/sets = %d/%v/%v, want 1/false/none", auth.probes, prompted, auth.setValues)
	}
}

func TestEnsureCanvasImportPippitAuthMissingKeyIsSideEffectFreeWhenNonInteractive(t *testing.T) {
	auth := &fakeImportAuthAPI{}

	err := ensureCanvasImportPippitAuth(context.Background(), auth, false, nil)
	if err == nil || !strings.Contains(err.Error(), "未找到小云雀 Access Key") {
		t.Fatalf("ensureCanvasImportPippitAuth() error = %v, want missing-key guidance", err)
	}
	if auth.probes != 0 || len(auth.setValues) != 0 {
		t.Fatalf("probes/sets = %d/%v, want no side effects", auth.probes, auth.setValues)
	}
}

func TestEnsureCanvasImportPippitAuthPromptsForMissingKeyWithoutProbingEmptyInput(t *testing.T) {
	auth := &fakeImportAuthAPI{}
	responses := []importAuthPromptResponse{
		{Action: importAuthPromptReplace, AccessKey: "  "},
		{Action: importAuthPromptReplace, AccessKey: " pasted-key "},
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
	if len(requests) != 2 || requests[0].HasAccessKey || requests[1].HasAccessKey {
		t.Fatalf("prompt requests = %#v, want two missing-key prompts", requests)
	}
	if !strings.Contains(requests[1].Failure, "不能为空") {
		t.Fatalf("second prompt failure = %q, want empty-key guidance", requests[1].Failure)
	}
	if auth.probes != 1 || strings.Join(auth.setValues, ",") != "pasted-key" {
		t.Fatalf("probes/sets = %d/%v, want one verified in-memory update", auth.probes, auth.setValues)
	}
}

func TestEnsureCanvasImportPippitAuthRetriesAndReplacesInvalidKey(t *testing.T) {
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
		{Action: importAuthPromptReplace, AccessKey: "replacement-key"},
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
	if auth.probes != 3 || strings.Join(auth.setValues, ",") != "replacement-key" {
		t.Fatalf("probes/sets = %d/%v, want retry then replacement", auth.probes, auth.setValues)
	}
	if len(requests) != 2 || !requests[0].HasAccessKey || !requests[1].HasAccessKey {
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
	if auth.probes != 1 || len(auth.setValues) != 0 {
		t.Fatalf("probes/sets = %d/%v, want one read-only probe and no update", auth.probes, auth.setValues)
	}
}

func TestEnsureCanvasImportPippitAuthDoesNotExposePromptErrorText(t *testing.T) {
	const candidate = "candidate-key-from-secret-input"

	err := ensureCanvasImportPippitAuth(
		context.Background(),
		&fakeImportAuthAPI{},
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

	cfg := &config.Config{BaseURL: server.URL, HTTPTimeout: time.Second}
	runner := common.NewRunner(cfg, nil)
	runner.Client = common.NewHTTPClient(
		cfg.BaseURL,
		cfg.HTTPTimeout,
		common.NewAccessKeyProviderAuthorizer(func() string { return runner.Config.AccessKey }),
	)
	auth := runnerImportAuthAPI{runner: runner}

	if err := auth.SetAccessKey(" pasted-key "); err != nil {
		t.Fatalf("SetAccessKey() error = %v", err)
	}
	if err := auth.Probe(context.Background()); err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if method != http.MethodPost || strings.Join(assetIDs, ",") != "9223372036854775807" {
		t.Fatalf("probe method/assets = %q/%v, want read-only Canvas query sentinel 9223372036854775807", method, assetIDs)
	}
	if cfg.AccessKey != "pasted-key" {
		t.Fatalf("Config.AccessKey = %q, want trimmed in-memory key", cfg.AccessKey)
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
