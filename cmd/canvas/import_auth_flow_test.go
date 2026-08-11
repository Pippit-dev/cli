package canvas

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	canvascore "github.com/Pippit-dev/pippit-cli/internal/canvas"
	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
)

type trackingImportAuthAPI struct {
	key         string
	events      *[]string
	probeErrors []error
	setValues   []string
}

func (auth *trackingImportAuthAPI) AccessKey() string { return auth.key }

func (auth *trackingImportAuthAPI) SetAccessKey(value string) error {
	auth.key = strings.TrimSpace(value)
	auth.setValues = append(auth.setValues, auth.key)
	return nil
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
	uploads int
}

func (api *expiringImportMediaAPI) PreflightUpload(context.Context) error { return nil }

func (api *expiringImportMediaAPI) Upload(
	context.Context,
	validatedImportMedia,
) (*canvascore.UploadResult, error) {
	api.uploads++
	if api.uploads == 1 {
		return nil, errors.New("HTTP 401")
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
	if err == nil || !strings.Contains(err.Error(), "未找到小云雀 Access Key") {
		t.Fatalf("runCanvasImport() error = %v, want missing-key guidance", err)
	}
	if sourceAuth.calls != 0 || len(exporter.inner.urls) != 0 || cacheTouched {
		t.Fatalf("source auth/export/cache side effects = %d/%d/%v, want all zero", sourceAuth.calls, len(exporter.inner.urls), cacheTouched)
	}
}

func TestCanvasImportAuthPromptsForPippitKeyThenChecksLibTV(t *testing.T) {
	const accessKey = "pasted-secret-access-key"
	events := []string{}
	pippit := &trackingImportAuthAPI{events: &events}
	source := &trackingSourceAuthenticator{events: &events}
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader(accessKey+"\n"), &stderr, false,
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
	if pippit.key != accessKey || len(pippit.setValues) != 1 {
		t.Fatalf("in-memory Access Key = %q / %v", pippit.key, pippit.setValues)
	}
	if strings.Contains(stderr.String(), accessKey) {
		t.Fatalf("stderr leaked pasted Access Key: %q", stderr.String())
	}
}

func TestCanvasImportAuthReplacesRejectedPippitKeyWithoutLeakingIt(t *testing.T) {
	const oldKey = "rejected-secret-key"
	const newKey = "replacement-secret-key"
	pippit := &trackingImportAuthAPI{
		key:         oldKey,
		probeErrors: []error{errors.New("HTTP 401 " + oldKey), nil},
	}
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader("2\n"+newKey+"\n"), &stderr, false,
	)

	err := preflightCanvasImportAuth(context.Background(), importDependencies{
		pippitAuth: pippit,
		sourceAuth: &trackingSourceAuthenticator{},
	}, prompts, &stderr)
	if err != nil {
		t.Fatalf("preflightCanvasImportAuth() error = %v", err)
	}
	if pippit.key != newKey {
		t.Fatalf("Access Key = %q, want replacement", pippit.key)
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
		key: "expired-key",
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
		context.Background(), strings.NewReader("2\nreplacement-key\n"), &stderr, false,
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
		key: "expired-key",
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
		context.Background(), strings.NewReader("2\nreplacement-key\n"), &stderr, false,
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
		key: "expired-key",
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
		context.Background(), strings.NewReader("2\nreplacement-key\n"), &stderr, false,
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
