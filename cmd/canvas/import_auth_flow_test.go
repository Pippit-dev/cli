package canvas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	canvascore "github.com/Pippit-dev/pippit-cli/internal/canvas"
	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
	"github.com/Pippit-dev/pippit-cli/internal/common"
)

type trackingImportAuthAPI struct {
	key             string
	loginKey        string
	events          *[]string
	probeErrors     []error
	loginErrors     []error
	logins          int
	explicit        bool
	credentialScope string
	loginScopes     []string
	loginOptions    []importAuthLoginOptions
}

func (auth *trackingImportAuthAPI) AccessKey(context.Context) (string, error) { return auth.key, nil }

func (auth *trackingImportAuthAPI) Login(_ context.Context, _ io.Writer, options importAuthLoginOptions) error {
	auth.logins++
	auth.loginOptions = append(auth.loginOptions, options)
	if len(auth.loginErrors) > 0 {
		err := auth.loginErrors[0]
		auth.loginErrors = auth.loginErrors[1:]
		if err != nil {
			return err
		}
	}
	if auth.loginKey == "" {
		auth.loginKey = "browser-managed-key"
	}
	auth.key = auth.loginKey
	if len(auth.loginScopes) > 0 {
		auth.credentialScope = auth.loginScopes[0]
		auth.loginScopes = auth.loginScopes[1:]
	}
	return nil
}

func (auth *trackingImportAuthAPI) HasExplicitAccessKey() bool { return auth.explicit }

func (auth *trackingImportAuthAPI) CredentialScope(context.Context) (string, error) {
	if auth.credentialScope != "" {
		return auth.credentialScope, nil
	}
	return "browser-device-scope", nil
}

func (auth *trackingImportAuthAPI) Probe(context.Context) error {
	if auth.events != nil {
		*auth.events = append(*auth.events, "pippit-auth")
	}
	if len(auth.probeErrors) == 0 {
		return nil
	}
	err := auth.probeErrors[0]
	auth.probeErrors = auth.probeErrors[1:]
	return err
}

type trackingSourceAuthenticator struct {
	events *[]string
	errors []error
	calls  int
}

func (auth *trackingSourceAuthenticator) Authenticate(context.Context, bool, io.Writer) error {
	auth.calls++
	if auth.events != nil {
		*auth.events = append(*auth.events, "libtv-auth")
	}
	if len(auth.errors) == 0 {
		return nil
	}
	err := auth.errors[0]
	auth.errors = auth.errors[1:]
	return err
}

type trackingImportExporter struct {
	inner        *fakeImportExporter
	events       *[]string
	exportErrors []error
}

type expiringImportMediaAPI struct {
	uploads         int
	uploadSucceeds  bool
	preflights      int
	preflightErrors []error
}

func (api *expiringImportMediaAPI) PreflightUpload(context.Context) error {
	api.preflights++
	if len(api.preflightErrors) == 0 {
		return nil
	}
	err := api.preflightErrors[0]
	api.preflightErrors = api.preflightErrors[1:]
	return err
}

func (api *expiringImportMediaAPI) Upload(
	context.Context,
	validatedImportMedia,
) (*canvascore.UploadResult, error) {
	api.uploads++
	if api.uploads == 1 && !api.uploadSucceeds {
		return nil, fmt.Errorf("HTTP 401: %w", internal_auth.ErrCredentialRejected)
	}
	return &canvascore.UploadResult{
		State: canvascore.StateReady, AssetID: "asset-after-reauth", PippitAssetID: "pippit-after-reauth",
	}, nil
}

func (*expiringImportMediaAPI) Query(context.Context, string) (bool, error) { return true, nil }

type sequencedImportExecutor struct {
	results []*canvasplan.ExecutionResult
	errors  []error
	calls   int
}

type sequencedReconcileExecutor struct {
	results []*canvasplan.ExecutionResult
	errors  []error
	calls   int
}

func (*sequencedReconcileExecutor) Execute(
	context.Context,
	canvasplan.Plan,
	canvasplan.ResolvedMediaSet,
	canvasplan.ExecuteOptions,
) (*canvasplan.ExecutionResult, error) {
	return nil, errors.New("unexpected Execute call during journal reconciliation")
}

func (executor *sequencedReconcileExecutor) Reconcile(
	context.Context,
	string,
	canvasplan.Plan,
	canvasplan.ResolvedMediaSet,
) (*canvasplan.ExecutionResult, error) {
	executor.calls++
	if len(executor.results) == 0 {
		return nil, errors.New("unexpected extra Reconcile call")
	}
	result := executor.results[0]
	executor.results = executor.results[1:]
	if len(executor.errors) == 0 {
		return result, nil
	}
	err := executor.errors[0]
	executor.errors = executor.errors[1:]
	return result, err
}

func (executor *sequencedImportExecutor) Execute(
	context.Context,
	canvasplan.Plan,
	canvasplan.ResolvedMediaSet,
	canvasplan.ExecuteOptions,
) (*canvasplan.ExecutionResult, error) {
	executor.calls++
	if len(executor.results) == 0 {
		return nil, errors.New("unexpected extra Execute call")
	}
	result := executor.results[0]
	executor.results = executor.results[1:]
	if len(executor.errors) == 0 {
		return result, nil
	}
	err := executor.errors[0]
	executor.errors = executor.errors[1:]
	return result, err
}

func (*sequencedImportExecutor) Reconcile(
	context.Context,
	string,
	canvasplan.Plan,
	canvasplan.ResolvedMediaSet,
) (*canvasplan.ExecutionResult, error) {
	return nil, canvasplan.ErrReconcileNotEligible
}

func (exporter *trackingImportExporter) Export(
	ctx context.Context,
	sourceURL string,
	outputDir string,
	stderr io.Writer,
) (*libTVExportResult, error) {
	*exporter.events = append(*exporter.events, "export")
	if len(exporter.exportErrors) != 0 {
		err := exporter.exportErrors[0]
		exporter.exportErrors = exporter.exportErrors[1:]
		return nil, err
	}
	return exporter.inner.Export(ctx, sourceURL, outputDir, stderr)
}

func TestCanvasImportPreflightsBothAccountsBeforeExport(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	events := []string{}
	innerExporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	exporter := &trackingImportExporter{inner: innerExporter, events: &events}
	media := &fakeImportMediaAPI{}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, innerExporter, media, executor)
	deps.pippitAuth = &trackingImportAuthAPI{key: "configured-key", events: &events}
	deps.sourceAuth = &trackingSourceAuthenticator{events: &events}
	deps.exporter = exporter

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, io.Discard, nil)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified {
		t.Fatalf("runCanvasImport() result = %#v, want verified", result)
	}
	if got := strings.Join(events, ","); got != "pippit-auth,libtv-auth,export,pippit-auth" {
		t.Fatalf("auth/export order = %q, want both auth probes before export and Pippit recheck before Canvas writes", got)
	}
}

func TestCanvasImportMissingPippitKeyStopsBeforeLibTVOrFilesystemSideEffects(t *testing.T) {
	cacheTouched := false
	sourceAuth := &trackingSourceAuthenticator{}
	exporter := &trackingImportExporter{inner: &fakeImportExporter{}, events: &[]string{}}
	deps := importDependencies{
		pippitAuth: &trackingImportAuthAPI{},
		sourceAuth: sourceAuth,
		exporter:   exporter,
		userCacheDir: func() (string, error) {
			cacheTouched = true
			return t.TempDir(), nil
		},
	}

	_, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "pippit-tool-cli login") {
		t.Fatalf("runCanvasImport() error = %v, want missing-key guidance", err)
	}
	if sourceAuth.calls != 0 || len(exporter.inner.urls) != 0 || cacheTouched {
		t.Fatalf("source auth/export/cache side effects = %d/%d/%v, want all zero", sourceAuth.calls, len(exporter.inner.urls), cacheTouched)
	}
}

func TestCanvasImportAuthOpensPippitBrowserLoginThenChecksLibTV(t *testing.T) {
	events := []string{}
	pippit := &trackingImportAuthAPI{events: &events, loginKey: "browser-managed-key"}
	source := &trackingSourceAuthenticator{events: &events}
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader(""), &stderr, false,
	)

	err := preflightCanvasImportAuth(context.Background(), importDependencies{
		pippitAuth: pippit,
		sourceAuth: source,
	}, prompts, &stderr)
	if err != nil {
		t.Fatalf("preflightCanvasImportAuth() error = %v", err)
	}
	if got := strings.Join(events, ","); got != "pippit-auth,libtv-auth" {
		t.Fatalf("auth order = %q, want Pippit then LibTV", got)
	}
	if pippit.key != "browser-managed-key" || pippit.logins != 1 {
		t.Fatalf("browser credential/logins = %q/%d", pippit.key, pippit.logins)
	}
	if strings.Contains(stderr.String(), pippit.key) {
		t.Fatalf("stderr leaked browser-managed Access Key: %q", stderr.String())
	}
}

func TestCanvasImportAuthReauthorizesRejectedManagedCredentialWithoutLeakingIt(t *testing.T) {
	const oldKey = "rejected-secret-key"
	const newKey = "browser-replacement-key"
	pippit := &trackingImportAuthAPI{
		key:         oldKey,
		loginKey:    newKey,
		probeErrors: []error{errors.New("HTTP 401 " + oldKey), nil},
	}
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader("1\n"), &stderr, false,
	)

	err := preflightCanvasImportAuth(context.Background(), importDependencies{
		pippitAuth: pippit,
		sourceAuth: &trackingSourceAuthenticator{},
	}, prompts, &stderr)
	if err != nil {
		t.Fatalf("preflightCanvasImportAuth() error = %v", err)
	}
	if pippit.key != newKey || pippit.logins != 1 {
		t.Fatalf("browser credential/logins = %q/%d, want replacement once", pippit.key, pippit.logins)
	}
	if strings.Contains(stderr.String(), oldKey) || strings.Contains(stderr.String(), newKey) {
		t.Fatalf("stderr leaked an Access Key: %q", stderr.String())
	}
}

func TestCanvasImportAuthRetriesRecoverableLibTVFailure(t *testing.T) {
	source := &trackingSourceAuthenticator{errors: []error{errors.New("浏览器授权暂时失败"), nil}}
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader("1\n"), &stderr, false,
	)

	err := preflightCanvasImportAuth(context.Background(), importDependencies{
		pippitAuth: &trackingImportAuthAPI{key: "configured-key"},
		sourceAuth: source,
	}, prompts, &stderr)
	if err != nil {
		t.Fatalf("preflightCanvasImportAuth() error = %v", err)
	}
	if source.calls != 2 {
		t.Fatalf("LibTV auth calls = %d, want retry in the same process", source.calls)
	}
	for _, expected := range []string{"LibTV 授权未完成", "重新打开浏览器授权", "授权校验通过"} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf("stderr missing %q: %q", expected, stderr.String())
		}
	}
}

func TestCanvasImportRetriesLibTVExportAfterRecheckingAuth(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	events := []string{}
	innerExporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	exporter := &trackingImportExporter{
		inner: innerExporter, events: &events,
		exportErrors: []error{errors.New("LibTV 网络暂时不可用")},
	}
	source := &trackingSourceAuthenticator{events: &events}
	deps := testImportDependencies(temp, innerExporter, &fakeImportMediaAPI{}, &fakeImportExecutor{result: verifiedImportResult()})
	deps.pippitAuth = &trackingImportAuthAPI{key: "configured-key", events: &events}
	deps.sourceAuth = source
	deps.exporter = exporter
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader("1\n"), &stderr, false,
	)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, &stderr, prompts)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified {
		t.Fatalf("runCanvasImport() result = %#v, want verified", result)
	}
	if source.calls != 2 {
		t.Fatalf("LibTV auth calls = %d, want preflight plus retry recheck", source.calls)
	}
	if got := strings.Join(events, ","); got != "pippit-auth,libtv-auth,export,libtv-auth,export,pippit-auth" {
		t.Fatalf("events = %q, want auth before each export attempt", got)
	}
}

func TestCanvasImportReauthorizesDuringMediaWithoutBlindUploadReplay(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &expiringImportMediaAPI{}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, exporter, media, executor)
	deps.pippitAuth = &trackingImportAuthAPI{
		key: "expired-key",
		probeErrors: []error{
			nil,
			errors.New("HTTP 401"),
			nil,
			nil,
		},
	}
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader("2\nreplacement-key\n"), &stderr, false,
	)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, &stderr, prompts)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified {
		t.Fatalf("runCanvasImport() result = %#v, want verified", result)
	}
	if media.uploads != 2 {
		t.Fatalf("Upload calls = %d, want one explicitly rejected attempt and one post-auth upload", media.uploads)
	}
	if strings.Contains(stderr.String(), "expired-key") || strings.Contains(stderr.String(), "replacement-key") {
		t.Fatalf("stderr leaked Access Key: %q", stderr.String())
	}
}

func TestCanvasImportReauthorizesWhenCredentialExpiresAfterExport(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &expiringImportMediaAPI{uploadSucceeds: true, preflightErrors: []error{
		fmt.Errorf("credential vanished after export: %w", internal_auth.ErrCredentialExpired),
		nil,
	}}
	pippit := &trackingImportAuthAPI{key: "expired-key", loginKey: "replacement-key"}
	deps := testImportDependencies(temp, exporter, media, &fakeImportExecutor{result: verifiedImportResult()})
	deps.pippitAuth = pippit
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(context.Background(), strings.NewReader(""), &stderr, false)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, &stderr, prompts)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified || media.uploads != 1 || pippit.logins != 1 {
		t.Fatalf("result/uploads/logins = %#v/%d/%d", result, media.uploads, pippit.logins)
	}
	if len(pippit.loginOptions) != 1 || !pippit.loginOptions[0].ForceRefresh ||
		pippit.loginOptions[0].ExpectedCredentialScope != "browser-device-scope" {
		t.Fatalf("login options = %#v, want task-bound forced refresh", pippit.loginOptions)
	}
}

func TestCanvasImportRejectsDifferentAccountDuringReauthentication(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &expiringImportMediaAPI{}
	pippit := &trackingImportAuthAPI{
		key:             "account-a-key",
		loginKey:        "account-b-key",
		credentialScope: "account-a-scope",
		loginScopes:     []string{"account-b-scope"},
	}
	deps := testImportDependencies(temp, exporter, media, &fakeImportExecutor{result: verifiedImportResult()})
	deps.pippitAuth = pippit
	deps.authScope = pippit.CredentialScope
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(context.Background(), strings.NewReader(""), &stderr, false)

	_, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, &stderr, prompts)
	if !errors.Is(err, internal_auth.ErrCredentialAccountMismatch) {
		t.Fatalf("runCanvasImport() error = %v, want account mismatch", err)
	}
	if media.uploads != 1 || pippit.logins != 1 {
		t.Fatalf("uploads/logins = %d/%d, want stop immediately after first rejected request and reauth", media.uploads, pippit.logins)
	}
	if len(pippit.loginOptions) != 1 || pippit.loginOptions[0].ExpectedCredentialScope != "account-a-scope" {
		t.Fatalf("login options = %#v, want account-a binding", pippit.loginOptions)
	}
}

func TestCanvasImportWaitsForAcceptedCreateInSameProcess(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	executor := &sequencedImportExecutor{results: []*canvasplan.ExecutionResult{
		{State: canvasplan.StateCreatePending, JournalPath: filepath.Join(temp, "pending.json")},
		verifiedImportResult(),
	}}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.mediaPoll = time.Millisecond
	var stderr bytes.Buffer

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, &stderr, nil)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified || executor.calls != 2 {
		t.Fatalf("result/calls = %#v/%d, want same-process pending resume", result, executor.calls)
	}
	if !strings.Contains(stderr.String(), "不会重复创建项目") {
		t.Fatalf("stderr missing safe pending progress: %q", stderr.String())
	}
}

func TestCanvasImportReauthorizesWhileWaitingForAcceptedCreate(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	executor := &sequencedImportExecutor{results: []*canvasplan.ExecutionResult{
		{
			State:       canvasplan.StateCreatePending,
			JournalPath: filepath.Join(temp, "pending.json"),
			Warning:     "poll canvas creation: HTTP 401",
		},
		verifiedImportResult(),
	}}
	pippit := &trackingImportAuthAPI{
		key:      "expired-key",
		loginKey: "replacement-key",
		probeErrors: []error{
			nil,
			nil,
			errors.New("HTTP 401"),
			nil,
		},
	}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.pippitAuth = pippit
	deps.mediaPoll = time.Millisecond
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader("1\n"), &stderr, false,
	)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, &stderr, prompts)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified || executor.calls != 2 {
		t.Fatalf("result/calls = %#v/%d, want reauthorized same-process create resume", result, executor.calls)
	}
	if pippit.key != "replacement-key" || !strings.Contains(stderr.String(), "不会重复创建") {
		t.Fatalf("key/stderr = %q/%q, want safe create reauthorization", pippit.key, stderr.String())
	}
}

func TestCanvasImportRealInitialCreateRejectionReauthenticatesFromInitialized(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path != canvascore.CreatePath {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer expired-ak" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		http.Error(writer, "expired", http.StatusUnauthorized)
	}))
	defer server.Close()
	client := common.NewHTTPClient(server.URL, time.Second, common.NewAccessKeyAuthorizer("expired-ak"))
	runner := common.NewRunner(nil, client)
	pippit := &trackingImportAuthAPI{
		key:         "expired-ak",
		loginErrors: []error{errors.New("stop after observing browser reauthentication")},
	}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, runnerImportExecutor{executor: canvasplan.NewExecutor(runner)})
	deps.pippitAuth = pippit
	journalPath := filepath.Join(temp, "real-create-auth.journal.json")
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(context.Background(), strings.NewReader(""), &stderr, false)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL, JournalPath: journalPath, JournalExplicit: true,
	}, deps, &stderr, prompts)
	if err == nil || !strings.Contains(err.Error(), "网页重新授权失败") {
		t.Fatalf("runCanvasImport() result/error = %#v/%v, want browser reauthentication attempt", result, err)
	}
	if result == nil || result.State != canvasplan.StateInitialized || pippit.logins != 1 || requests != 1 {
		t.Fatalf("result/logins/requests = %#v/%d/%d, want initialized/1/1", result, pippit.logins, requests)
	}
	payload, readErr := os.ReadFile(journalPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	journal := &canvasplan.Journal{}
	if err := json.Unmarshal(payload, journal); err != nil || journal.State != canvasplan.StateInitialized || journal.Create != nil {
		t.Fatalf("journal/error = %#v/%v, want durable initialized state", journal, err)
	}
	if !strings.Contains(stderr.String(), "断点已保存") {
		t.Fatalf("stderr missing safe retry guidance: %q", stderr.String())
	}
}

func TestCanvasImportReauthorizesAmbiguousApplyThenQueriesWithoutReplay(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	executor := &sequencedImportExecutor{
		results: []*canvasplan.ExecutionResult{
			{State: canvasplan.StateApplyAmbiguous, JournalPath: filepath.Join(temp, "ambiguous.json")},
			verifiedImportResult(),
		},
		errors: []error{errors.New("canvas apply failed; exact query-back failed: HTTP 401")},
	}
	pippit := &trackingImportAuthAPI{
		key:      "expired-key",
		loginKey: "replacement-key",
		probeErrors: []error{
			nil,
			nil,
			errors.New("HTTP 401"),
			nil,
		},
	}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.pippitAuth = pippit
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader("1\n"), &stderr, false,
	)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, &stderr, prompts)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified || executor.calls != 2 {
		t.Fatalf("result/calls = %#v/%d, want auth recovery followed by safe query reconciliation", result, executor.calls)
	}
	if pippit.key != "replacement-key" || !strings.Contains(stderr.String(), "从安全状态继续") {
		t.Fatalf("key/stderr = %q/%q, want safe apply reauthorization", pippit.key, stderr.String())
	}
}

func TestCanvasImportKeepsQueryingAmbiguousApplyWithoutReplaying(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	executor := &sequencedImportExecutor{
		results: []*canvasplan.ExecutionResult{
			{State: canvasplan.StateApplyAmbiguous, JournalPath: filepath.Join(temp, "ambiguous.json")},
			verifiedImportResult(),
		},
		errors: []error{errors.New("temporary exact query-back failure")},
	}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.mediaPoll = time.Millisecond
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader(""), &stderr, false,
	)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, &stderr, prompts)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified || executor.calls != 2 {
		t.Fatalf("result/calls = %#v/%d, want safe read-only recovery", result, executor.calls)
	}
	if !strings.Contains(stderr.String(), "继续只读回查") {
		t.Fatalf("stderr = %q, want read-only recovery progress", stderr.String())
	}
}

func TestCanvasImportReauthorizesExistingAmbiguousJournal(t *testing.T) {
	temp := t.TempDir()
	_, exporter, journalPath := prepareSourceBoundResumeFixture(t, temp)
	executor := &sequencedReconcileExecutor{
		results: []*canvasplan.ExecutionResult{
			{State: canvasplan.StateApplyAmbiguous, Warning: "query assets failed: ret=1015"},
			verifiedImportResult(),
		},
		errors: []error{errors.New("query assets failed: ret=1015")},
	}
	pippit := &trackingImportAuthAPI{
		key:      "expired-key",
		loginKey: "replacement-key",
		probeErrors: []error{
			nil,
			nil,
			errors.New("ret=1015"),
			nil,
		},
	}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.pippitAuth = pippit
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader("1\n"), &stderr, false,
	)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL, JournalPath: journalPath,
		JournalExplicit: true, AcceptDegradations: true,
	}, deps, &stderr, prompts)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified || executor.calls != 2 {
		t.Fatalf("result/calls = %#v/%d, want reauthorized journal reconciliation", result, executor.calls)
	}
	if pippit.key != "replacement-key" || !strings.Contains(stderr.String(), "恢复画布断点期间失效") {
		t.Fatalf("key/stderr = %q/%q, want safe journal reauthorization", pippit.key, stderr.String())
	}
}

func TestCanvasImportKeepsQueryingExistingAmbiguousJournal(t *testing.T) {
	temp := t.TempDir()
	_, exporter, journalPath := prepareSourceBoundResumeFixture(t, temp)
	executor := &sequencedReconcileExecutor{
		results: []*canvasplan.ExecutionResult{
			{State: canvasplan.StateApplyAmbiguous, Warning: "temporary query failure"},
			verifiedImportResult(),
		},
		errors: []error{errors.New("temporary query failure")},
	}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.mediaPoll = time.Millisecond
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader(""), &stderr, false,
	)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL, JournalPath: journalPath,
		JournalExplicit: true, AcceptDegradations: true,
	}, deps, &stderr, prompts)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v", err)
	}
	if result == nil || result.State != canvasplan.StateVerified || executor.calls != 2 {
		t.Fatalf("result/calls = %#v/%d, want safe existing-journal query recovery", result, executor.calls)
	}
	if !strings.Contains(stderr.String(), "继续只读回查断点") {
		t.Fatalf("stderr = %q, want read-only journal recovery progress", stderr.String())
	}
}

func TestCanvasImportDoesNotRetryLocalReconcileMismatchWithHistoricalWarning(t *testing.T) {
	temp := t.TempDir()
	_, exporter, journalPath := prepareSourceBoundResumeFixture(t, temp)
	executor := &sequencedReconcileExecutor{
		results: []*canvasplan.ExecutionResult{{
			State:   canvasplan.StateApplyAmbiguous,
			Warning: "historical ambiguous apply warning",
		}},
		errors: []error{errors.New("CanvasPlan or resolved media changed after journal creation")},
	}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader(""), &stderr, false,
	)

	_, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL, JournalPath: journalPath,
		JournalExplicit: true, AcceptDegradations: true,
	}, deps, &stderr, prompts)
	if err == nil || !strings.Contains(err.Error(), "changed after journal creation") {
		t.Fatalf("runCanvasImport() error = %v, want fail-closed input mismatch", err)
	}
	if executor.calls != 1 || strings.Contains(stderr.String(), "继续只读回查断点") {
		t.Fatalf("calls/stderr = %d/%q, want no retry for local mismatch", executor.calls, stderr.String())
	}
}

func TestPrepareCanvasImportKeepsPromptSessionForAuthWithExplicitFlags(t *testing.T) {
	prepared, prompts, err := prepareCanvasImportOptions(
		context.Background(),
		strings.NewReader(""),
		importOptions{Provider: "libtv", SourceURL: testLibTVURL},
		func(io.Reader) bool { return true },
		io.Discard,
	)
	if err != nil {
		t.Fatalf("prepareCanvasImportOptions() error = %v", err)
	}
	if prompts == nil || prepared.SourceURL != testLibTVURL {
		t.Fatalf("prepared/prompts = %#v/%v, want auth-capable interactive session", prepared, prompts)
	}
}
