package canvas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	canvascore "github.com/Pippit-dev/pippit-cli/internal/canvas"
	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type fakeImportExporter struct {
	plan       canvasplan.Plan
	mediaBytes map[string][]byte
	urls       []string
	bundles    []string
	authCalls  int
	authErrors []error
}

func (exporter *fakeImportExporter) Authenticate(context.Context, bool, io.Writer) error {
	exporter.authCalls++
	if len(exporter.authErrors) == 0 {
		return nil
	}
	err := exporter.authErrors[0]
	exporter.authErrors = exporter.authErrors[1:]
	return err
}

func (exporter *fakeImportExporter) Export(
	_ context.Context,
	sourceURL, outputDir string,
	_ io.Writer,
) (*libTVExportResult, error) {
	exporter.urls = append(exporter.urls, sourceURL)
	exporter.bundles = append(exporter.bundles, outputDir)
	if err := os.MkdirAll(filepath.Join(outputDir, "media"), 0o700); err != nil {
		return nil, err
	}
	for relative, payload := range exporter.mediaBytes {
		path := filepath.Join(outputDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			return nil, err
		}
	}
	planPath := filepath.Join(outputDir, "plan.json")
	if err := writeTestJSON(planPath, exporter.plan); err != nil {
		return nil, err
	}
	snapshotPath := filepath.Join(outputDir, "snapshot.json")
	if err := writeTestJSON(snapshotPath, map[string]string{"schema": "test"}); err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(outputDir, "media-manifest.json")
	if err := writeTestJSON(manifestPath, map[string]any{"media": []any{}}); err != nil {
		return nil, err
	}
	media := make([]libTVExportMedia, 0, len(exporter.plan.RequiredMedia))
	for _, requirement := range exporter.plan.RequiredMedia {
		media = append(media, libTVExportMedia{
			LogicalID: requirement.LogicalID,
			MediaType: requirement.MediaType,
			LocalPath: filepath.Join(outputDir, filepath.FromSlash(requirement.LocalPath)),
		})
	}
	return &libTVExportResult{
		BundleDir:         outputDir,
		SnapshotPath:      snapshotPath,
		MediaManifestPath: manifestPath,
		PlanPath:          planPath,
		Schema:            libTVExportResultSchema,
		PlanSchema:        canvasplan.PlanSchema,
		Source:            exporter.plan.Source,
		Media:             media,
		MediaCount:        len(exporter.plan.RequiredMedia),
		NodeCount:         len(exporter.plan.Nodes),
		GroupCount:        len(exporter.plan.Groups),
		EdgeCount:         len(exporter.plan.Edges),
		DegradationCount:  len(exporter.plan.Degradations),
	}, nil
}

type fakeImportMediaAPI struct {
	uploads     int
	queries     int
	uploadState string
	uploadErr   error
	queryErr    error
	queryReady  []bool
}

func (api *fakeImportMediaAPI) Upload(_ context.Context, _ validatedImportMedia) (*canvascore.UploadResult, error) {
	api.uploads++
	if api.uploadErr != nil {
		return nil, api.uploadErr
	}
	state := api.uploadState
	if state == "" {
		state = canvascore.StateReady
	}
	return &canvascore.UploadResult{
		State: state, AssetID: "asset-1", PippitAssetID: "pippit-1",
	}, nil
}

func (api *fakeImportMediaAPI) Query(context.Context, string) (bool, error) {
	api.queries++
	if api.queryErr != nil {
		return false, api.queryErr
	}
	if len(api.queryReady) == 0 {
		return true, nil
	}
	ready := api.queryReady[0]
	api.queryReady = api.queryReady[1:]
	return ready, nil
}

type panickingImportMediaAPI struct {
	uploads int
}

func (api *panickingImportMediaAPI) Upload(context.Context, validatedImportMedia) (*canvascore.UploadResult, error) {
	api.uploads++
	panic("simulated process exit during upload")
}

func (*panickingImportMediaAPI) Query(context.Context, string) (bool, error) {
	return true, nil
}

type missingAKPreflightMediaAPI struct {
	uploads int
}

func (*missingAKPreflightMediaAPI) PreflightUpload(context.Context) error {
	return errors.New("XYQ_ACCESS_KEY 缺失")
}

func (api *missingAKPreflightMediaAPI) Upload(context.Context, validatedImportMedia) (*canvascore.UploadResult, error) {
	api.uploads++
	return nil, errors.New("must not be called")
}

func (*missingAKPreflightMediaAPI) Query(context.Context, string) (bool, error) {
	return true, nil
}

type blockingImportMediaAPI struct {
	started chan struct{}
	release chan struct{}
	uploads int
}

func (api *blockingImportMediaAPI) Upload(context.Context, validatedImportMedia) (*canvascore.UploadResult, error) {
	api.uploads++
	close(api.started)
	<-api.release
	return &canvascore.UploadResult{
		State: canvascore.StateReady, AssetID: "asset-blocking", PippitAssetID: "pippit-blocking",
	}, nil
}

func (*blockingImportMediaAPI) Query(context.Context, string) (bool, error) {
	return true, nil
}

type fakeImportExecutor struct {
	calls             int
	reconcileCalls    int
	resolved          canvasplan.ResolvedMediaSet
	opts              canvasplan.ExecuteOptions
	result            *canvasplan.ExecutionResult
	reconcileJournal  string
	reconcilePlan     canvasplan.Plan
	reconcileResolved canvasplan.ResolvedMediaSet
	reconcileResult   *canvasplan.ExecutionResult
	reconcileErr      error
}

func (executor *fakeImportExecutor) Execute(
	_ context.Context,
	_ canvasplan.Plan,
	resolved canvasplan.ResolvedMediaSet,
	opts canvasplan.ExecuteOptions,
) (*canvasplan.ExecutionResult, error) {
	executor.calls++
	executor.resolved = resolved
	executor.opts = opts
	return executor.result, nil
}

func (executor *fakeImportExecutor) Reconcile(
	_ context.Context,
	journalPath string,
	plan canvasplan.Plan,
	resolved canvasplan.ResolvedMediaSet,
) (*canvasplan.ExecutionResult, error) {
	executor.reconcileCalls++
	executor.reconcileJournal = journalPath
	executor.reconcilePlan = plan
	executor.reconcileResolved = resolved
	return executor.reconcileResult, executor.reconcileErr
}

func TestImportCommandExportsUploadsDeduplicatesVerifiesAndOpens(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &fakeImportMediaAPI{}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	opened := ""
	deps := testImportDependencies(temp, exporter, media, executor)
	deps.openURL = func(_ context.Context, value string) error { opened = value; return nil }
	var stdout, stderr bytes.Buffer
	cmd := newImportCommand(&stdout, &stderr, deps)
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--from", "libtv",
		"--url", "https://liblib.tv/canvas?token=secret&spaceId=3872811&projectId=037A5C49E1B344E5ADBC899AD93FDCA9",
		"--open",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if got := exporter.urls[0]; got != "https://www.liblib.tv/canvas?projectId=037a5c49e1b344e5adbc899ad93fdca9&spaceId=3872811" {
		t.Fatalf("export URL = %q, want canonical URL without token", got)
	}
	if media.uploads != 1 {
		t.Fatalf("uploads = %d, want one upload for duplicate bytes", media.uploads)
	}
	if len(executor.resolved.Media) != 2 || executor.resolved.Media[0].PippitAssetID != executor.resolved.Media[1].PippitAssetID {
		t.Fatalf("resolved media = %#v, want two logical IDs sharing one uploaded asset", executor.resolved.Media)
	}
	if opened != executor.result.WebURL {
		t.Fatalf("opened = %q, want verified web URL", opened)
	}
	if strings.Count(stdout.String(), "\n") != 1 || !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("stdout = %q, want one JSON line", stdout.String())
	}
	if _, err := os.Stat(exporter.bundles[0]); !os.IsNotExist(err) {
		t.Fatalf("successful export bundle still exists: %v", err)
	}
	checkpointInfo, err := os.Stat(executor.opts.JournalPath + ".media.json")
	if err != nil || checkpointInfo.Mode().Perm() != 0o600 {
		t.Fatalf("media checkpoint info = (%v, %v), want mode 0600", checkpointInfo, err)
	}
}

func TestImportCommandFallsBackToExecuteForNormalVerificationFailure(t *testing.T) {
	temp := t.TempDir()
	_, exporter, journalPath := prepareSourceBoundResumeFixture(t, temp)
	executor := &fakeImportExecutor{
		result:          verifiedImportResult(),
		reconcileResult: &canvasplan.ExecutionResult{State: canvasplan.StateVerificationFailed},
		reconcileErr:    canvasplan.ErrReconcileNotEligible,
	}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	cmd := newImportCommand(io.Discard, io.Discard, deps)
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--from", "libtv", "--url", testLibTVURL, "--journal", journalPath,
		"--accept-degradations",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want normal executor fallback", err)
	}
	if executor.reconcileCalls != 1 || executor.calls != 1 {
		t.Fatalf(
			"input-reconcile/execute calls = %d/%d, want 1/1",
			executor.reconcileCalls, executor.calls,
		)
	}
}

func TestImportCommandInteractiveWizardUsesSafeDefaults(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &fakeImportMediaAPI{}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	opened := ""
	deps := testImportDependencies(temp, exporter, media, executor)
	deps.isInteractive = func(io.Reader) bool { return true }
	deps.openURL = func(_ context.Context, value string) error { opened = value; return nil }
	var stdout, stderr bytes.Buffer
	cmd := newImportCommand(&stdout, &stderr, deps)
	cmd.SetIn(strings.NewReader("\n" + testLibTVURL + "\n\n\n"))
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	wantURL := "https://www.liblib.tv/canvas?projectId=037a5c49e1b344e5adbc899ad93fdca9&spaceId=3872811"
	if len(exporter.urls) != 1 || exporter.urls[0] != wantURL {
		t.Fatalf("export URLs = %#v, want prompted LibTV URL", exporter.urls)
	}
	if opened != executor.result.WebURL {
		t.Fatalf("opened = %q, want wizard default Yes", opened)
	}
	for _, message := range []string{
		"导入来源：", "1) LibTV（默认）", "LibTV 画布链接", "断点续跑记录：",
		"1) 自动生成（推荐，默认）", "2) 自定义路径", "导入完成后：",
		"1) 打开画布（默认）", "2) 暂不打开",
		"断点续跑记录：" + executor.opts.JournalPath,
		`素材进度：已处理=1/2，剩余=1，状态=已上传，文件="one.png"`,
		`素材进度：已处理=2/2，剩余=0，状态=已复用，文件="two.png"`,
		`素材进度：已处理=0/2，剩余=2，状态=正在上传，文件="one.png"`,
		"阶段：正在创建或续跑画布、写入节点与连线，并回读验证远端画布素材…",
		"阶段：画布导入已通过回读验证。",
	} {
		if !strings.Contains(stderr.String(), message) {
			t.Fatalf("stderr missing %q:\n%s", message, stderr.String())
		}
	}
	if strings.Count(stdout.String(), "\n") != 1 || !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("stdout = %q, want one final JSON line", stdout.String())
	}
}

func TestImportCommandHelpUsesChineseCopy(t *testing.T) {
	var stdout bytes.Buffer
	cmd := newImportCommand(&stdout, io.Discard, importDependencies{})
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, expected := range []string{
		"将外部项目导入个人漫剧画布",
		"不传来源参数时会进入交互式向导",
		"导入来源（当前仅支持 libtv）",
		"来源项目链接",
		"断点续跑记录路径（省略时自动生成）",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("help output missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestImportCommandInteractiveWizardRetriesAndUsesNumberedCustomChoices(t *testing.T) {
	temp := t.TempDir()
	journalDirectory := filepath.Join(temp, "custom-state")
	if err := os.MkdirAll(journalDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(journalDirectory, "import.journal.json")
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	opened := false
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.isInteractive = func(io.Reader) bool { return true }
	deps.openURL = func(context.Context, string) error { opened = true; return nil }
	var stdout, stderr bytes.Buffer
	cmd := newImportCommand(&stdout, &stderr, deps)
	cmd.SetIn(strings.NewReader(strings.Join([]string{
		"9", "1", testLibTVURL, "9", "2", journalPath, "maybe", "2", "",
	}, "\n")))
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if executor.opts.JournalPath != journalPath {
		t.Fatalf("journal = %q, want custom path %q", executor.opts.JournalPath, journalPath)
	}
	if opened {
		t.Fatal("wizard option 2 unexpectedly opened the Canvas")
	}
	if got := strings.Count(stderr.String(), "请输入 1 到"); got != 3 {
		t.Fatalf("invalid choice messages = %d, want 3:\n%s", got, stderr.String())
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("stdout = %q, want final JSON", stdout.String())
	}
}

func TestImportCommandInteractiveWizardWarnsAndContinuesDegradations(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, true)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.isInteractive = func(io.Reader) bool { return true }
	var stdout, stderr bytes.Buffer
	cmd := newImportCommand(&stdout, &stderr, deps)
	cmd.SetIn(strings.NewReader("\n" + testLibTVURL + "\n\nn\n"))
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "已知的非致命能力降级") ||
		!strings.Contains(stderr.String(), "空素材占位或语义降级") ||
		!strings.Contains(stderr.String(), "degradation_count") {
		t.Fatalf("stderr = %q, want auditable automatic degradation warning", stderr.String())
	}
	if executor.calls != 1 || !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("executor/stdout = %d/%q, want completed interactive import", executor.calls, stdout.String())
	}
}

func TestImportCommandInteractiveWizardHonorsExplicitDegradationRejection(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, true)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.isInteractive = func(io.Reader) bool { return true }
	var stderr bytes.Buffer
	cmd := newImportCommand(io.Discard, &stderr, deps)
	cmd.SetIn(strings.NewReader("\n" + testLibTVURL + "\n\n\n"))
	cmd.SetArgs([]string{"--accept-degradations=false"})
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--accept-degradations") {
		t.Fatalf("Execute() error = %v, want explicit degradation rejection", err)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want no Canvas write after explicit rejection", executor.calls)
	}
	if strings.Contains(stderr.String(), "交互式导入将自动继续") {
		t.Fatalf("stderr = %q, explicit false must not be ignored by the wizard", stderr.String())
	}
}

func TestImportCommandInteractiveCustomJournalEOFIsActionable(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, &fakeImportExecutor{})
	deps.isInteractive = func(io.Reader) bool { return true }
	cmd := newImportCommand(io.Discard, io.Discard, deps)
	cmd.SetIn(strings.NewReader("\n" + testLibTVURL + "\n2\n"))
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "自定义断点记录路径") ||
		!strings.Contains(err.Error(), "请选择 1 自动生成") {
		t.Fatalf("Execute() error = %v, want actionable custom path EOF", err)
	}
	if len(exporter.urls) != 0 {
		t.Fatalf("exporter called before custom journal input completed: %#v", exporter.urls)
	}
}

func TestImportCommandMissingFlagsFailsActionablyWithoutInteractiveInput(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, &fakeImportExecutor{})
	cmd := newImportCommand(io.Discard, io.Discard, deps)
	cmd.SetIn(strings.NewReader(""))
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "当前输入不是交互式终端") ||
		!strings.Contains(err.Error(), "--from libtv --url") {
		t.Fatalf("Execute() error = %v, want actionable non-interactive flags", err)
	}
	if len(exporter.urls) != 0 {
		t.Fatalf("exporter called before required input validation: %#v", exporter.urls)
	}
}

func TestImportInputDoesNotTreatNullDeviceAsInteractive(t *testing.T) {
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if importInputIsInteractive(input) {
		t.Fatal("null device was incorrectly treated as an interactive terminal")
	}
}

func TestImportCommandInteractiveEOFMissingURLFailsBeforeExport(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, &fakeImportExecutor{})
	deps.isInteractive = func(io.Reader) bool { return true }
	cmd := newImportCommand(io.Discard, io.Discard, deps)
	cmd.SetIn(strings.NewReader("\n"))
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "尚未提供 LibTV 画布链接") ||
		!strings.Contains(err.Error(), "--from libtv --url") {
		t.Fatalf("Execute() error = %v, want actionable EOF guidance", err)
	}
	if len(exporter.urls) != 0 {
		t.Fatalf("exporter called after prompt EOF: %#v", exporter.urls)
	}
}

func TestImportCommandRejectsUnsafeExplicitJournalBeforeExport(t *testing.T) {
	for _, test := range []struct {
		name        string
		journal     string
		wantMessage string
	}{
		{name: "explicit empty", journal: "", wantMessage: "explicitly set but is empty"},
		{
			name:        "filesystem root child",
			journal:     filepath.Join(string(filepath.Separator), "import.journal.json"),
			wantMessage: "shell directory variable was empty",
		},
		{
			name:        "missing parent",
			journal:     filepath.Join(t.TempDir(), "not-created", "import.journal.json"),
			wantMessage: "mkdir -p",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			temp := t.TempDir()
			plan, mediaBytes := testImportPlan(t, false)
			exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
			deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, &fakeImportExecutor{})
			cmd := newImportCommand(io.Discard, io.Discard, deps)
			cmd.SetArgs([]string{
				"--from", "libtv", "--url", testLibTVURL, "--journal", test.journal,
			})
			cmd.SilenceUsage = true
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) ||
				!strings.Contains(err.Error(), "unset/omit --journal") {
				t.Fatalf("Execute() error = %v, want early journal guidance containing %q", err, test.wantMessage)
			}
			if len(exporter.urls) != 0 {
				t.Fatalf("exporter called before explicit journal validation: %#v", exporter.urls)
			}
		})
	}
}

func TestImportCommandRequiresExplicitDegradationAcceptanceAndKeepsBundle(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, true)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &fakeImportMediaAPI{}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, exporter, media, executor)
	var stdout, stderr bytes.Buffer
	cmd := newImportCommand(&stdout, &stderr, deps)
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--from", "libtv", "--url", testLibTVURL})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--accept-degradations") || !strings.Contains(err.Error(), exporter.bundles[0]) {
		t.Fatalf("Execute() error = %v, want inspectable degradation gate", err)
	}
	if media.uploads != 0 || executor.calls != 0 || stdout.Len() != 0 {
		t.Fatalf("side effects/stdout = (%d, %d, %q), want none", media.uploads, executor.calls, stdout.String())
	}
	if _, err := os.Stat(exporter.bundles[0]); err != nil {
		t.Fatalf("degraded export bundle was removed: %v", err)
	}
}

func TestImportCommandWaitsForProcessingUploadAndContinuesSameInvocation(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testSingleMediaPlan(t)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &fakeImportMediaAPI{
		uploadState: canvascore.StateProcessing,
		queryReady:  []bool{false, false, true},
	}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, exporter, media, executor)
	var stdout, stderr bytes.Buffer
	cmd := newImportCommand(&stdout, &stderr, deps)
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--from", "libtv", "--url", testLibTVURL})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if media.uploads != 1 || media.queries != 3 || executor.calls != 1 {
		t.Fatalf("upload/query/execute = %d/%d/%d, want 1/3/1 in one invocation", media.uploads, media.queries, executor.calls)
	}
	for _, progress := range []string{
		`素材进度：已处理=0/1，剩余=1，状态=正在处理，文件="one.png"`,
		`素材进度：已处理=0/1，剩余=1，状态=等待处理中，文件="one.png"`,
		`素材进度：已处理=1/1，剩余=0，状态=已上传，文件="one.png"`,
	} {
		if !strings.Contains(stderr.String(), progress) {
			t.Fatalf("stderr = %q, want progress %q", stderr.String(), progress)
		}
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("stdout = %q, want completed import JSON", stdout.String())
	}
}

func TestImportCommandContinuesAfterInteractiveMediaProcessingWindow(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testSingleMediaPlan(t)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &fakeImportMediaAPI{
		uploadState: canvascore.StateProcessing,
		queryReady:  []bool{false, true},
	}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, exporter, media, executor)
	deps.mediaPoll = 2 * time.Millisecond
	deps.mediaTimeout = time.Millisecond
	var stderr bytes.Buffer
	prompts := newImportPromptSessionWithTUI(
		context.Background(), strings.NewReader(""), &stderr, false,
	)

	result, err := runCanvasImport(context.Background(), importOptions{
		Provider: "libtv", SourceURL: testLibTVURL,
	}, deps, &stderr, prompts)
	if err != nil {
		t.Fatalf("runCanvasImport() error = %v, stderr = %s", err, stderr.String())
	}
	if result == nil || result.State != canvasplan.StateVerified || media.uploads != 1 || media.queries != 2 {
		t.Fatalf("result/upload/query = %#v/%d/%d, want one upload and continued queries", result, media.uploads, media.queries)
	}
	if !strings.Contains(stderr.String(), "继续只读查询，不会重复上传") {
		t.Fatalf("stderr = %q, want noninterrupting processing progress", stderr.String())
	}
}

func TestImportMediaResumesProcessingCheckpointWithoutUploading(t *testing.T) {
	opts := testMediaResolutionOptions(t)
	item := opts.Media[0]
	checkpoint := &mediaCheckpoint{
		Schema:     mediaCheckpointSchema,
		Source:     opts.Plan.Source,
		Target:     opts.Target,
		BundleDirs: []string{opts.BundleDir},
		Entries: []mediaCheckpointEntry{{
			LogicalID: item.LogicalID, MediaType: item.MediaType, SHA256: item.SHA256,
			Status: mediaStatusProcessing, AssetID: "asset-processing", PippitAssetID: "pippit-processing",
		}},
	}
	if err := saveMediaCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
		t.Fatal(err)
	}
	api := &fakeImportMediaAPI{queryReady: []bool{false, true}}
	var stderr bytes.Buffer
	resolved, err := resolveImportMedia(context.Background(), opts, api, &stderr)
	if err != nil {
		t.Fatalf("resolveImportMedia() error = %v, stderr = %s", err, stderr.String())
	}
	if api.uploads != 0 || api.queries != 2 || len(resolved.Media) != 1 {
		t.Fatalf("uploads/queries/resolved = %d/%d/%#v, want 0/2/one", api.uploads, api.queries, resolved.Media)
	}
	saved := readTestMediaCheckpoint(t, opts.CheckpointPath)
	if len(saved.Entries) != 1 || saved.Entries[0].Status != mediaStatusReady {
		t.Fatalf("checkpoint = %#v, want processing entry promoted to ready", saved)
	}
}

func TestImportMediaProcessingQueryAuthErrorStopsAndPreservesIDs(t *testing.T) {
	opts := testMediaResolutionOptions(t)
	item := opts.Media[0]
	checkpoint := &mediaCheckpoint{
		Schema:     mediaCheckpointSchema,
		Source:     opts.Plan.Source,
		Target:     opts.Target,
		BundleDirs: []string{opts.BundleDir},
		Entries: []mediaCheckpointEntry{{
			LogicalID: item.LogicalID, MediaType: item.MediaType, SHA256: item.SHA256,
			Status: mediaStatusProcessing, AssetID: "asset-durable", PippitAssetID: "pippit-durable",
		}},
	}
	if err := saveMediaCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
		t.Fatal(err)
	}
	api := &fakeImportMediaAPI{queryErr: errors.New("HTTP 401 Unauthorized")}
	_, err := resolveImportMedia(context.Background(), opts, api, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "读取或授权错误") ||
		!strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "持久化素材 ID 仍保存在") {
		t.Fatalf("resolveImportMedia() error = %v, want immediate explicit auth/query error", err)
	}
	if api.uploads != 0 || api.queries != 1 {
		t.Fatalf("uploads/queries = %d/%d, want 0/1 without polling or re-upload", api.uploads, api.queries)
	}
	saved := readTestMediaCheckpoint(t, opts.CheckpointPath)
	if len(saved.Entries) != 1 || saved.Entries[0].Status != mediaStatusProcessing ||
		saved.Entries[0].AssetID != "asset-durable" || saved.Entries[0].PippitAssetID != "pippit-durable" {
		t.Fatalf("checkpoint = %#v, want processing status and durable IDs preserved", saved)
	}
}

func TestLibTVPNGAIGCFingerprintNormalizesOnlyVolatileIDs(t *testing.T) {
	pixel := color.NRGBA{R: 20, G: 40, B: 60, A: 255}
	first := testPNGWithITXt(t, pixel, testLibTVAIGCMetadata("libtv"+strings.Repeat("a", 32)))
	second := testPNGWithITXt(t, pixel, testLibTVAIGCMetadata("libtv"+strings.Repeat("b", 32)))
	firstRaw := sha256.Sum256(first)
	secondRaw := sha256.Sum256(second)
	if firstRaw == secondRaw {
		t.Fatal("AIGC ID fixtures unexpectedly have the same raw SHA-256")
	}
	firstFingerprint, err := canonicalLibTVPNGFingerprint(first)
	if err != nil {
		t.Fatalf("canonicalLibTVPNGFingerprint(first) error = %v", err)
	}
	secondFingerprint, err := canonicalLibTVPNGFingerprint(second)
	if err != nil {
		t.Fatalf("canonicalLibTVPNGFingerprint(second) error = %v", err)
	}
	if firstFingerprint != secondFingerprint || !strings.HasPrefix(firstFingerprint, libTVPNGAIGCFingerprintPrefix) {
		t.Fatalf("fingerprints = %q/%q, want identical versioned AIGC fingerprints", firstFingerprint, secondFingerprint)
	}
}

func TestLibTVPNGAIGCFingerprintPreservesAllOtherPNGContent(t *testing.T) {
	pixel := color.NRGBA{R: 20, G: 40, B: 60, A: 255}
	metadata := testLibTVAIGCMetadata("libtv" + strings.Repeat("a", 32))
	baseline := testPNGWithITXt(t, pixel, metadata)
	baselineFingerprint, err := canonicalLibTVPNGFingerprint(baseline)
	if err != nil {
		t.Fatal(err)
	}
	idatChanged := testRewriteFirstPNGChunk(t, baseline, "IDAT", func(data []byte) {
		data[0] ^= 0x01
	})
	apngOne := testInsertPNGChunkBeforeIEND(t, baseline, "acTL", []byte{0, 0, 0, 1, 0, 0, 0, 0})
	apngTwo := testInsertPNGChunkBeforeIEND(t, baseline, "acTL", []byte{0, 0, 0, 1, 0, 0, 0, 1})
	cases := map[string][]byte{
		"stable AIGC field": testPNGWithITXt(
			t, pixel, strings.Replace(metadata, `"Label":"1"`, `"Label":"2"`, 1),
		),
		"IDAT data":  idatChanged,
		"APNG chunk": apngOne,
		"pixel data": testPNGWithITXt(
			t, color.NRGBA{R: 21, G: 40, B: 60, A: 255},
			metadata,
		),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			fingerprint, err := canonicalLibTVPNGFingerprint(payload)
			if err != nil {
				t.Fatalf("canonicalLibTVPNGFingerprint() error = %v", err)
			}
			if fingerprint == baselineFingerprint {
				t.Fatalf("fingerprint = %q, want change to affect canonical identity", fingerprint)
			}
		})
	}
	apngOneFingerprint, err := canonicalLibTVPNGFingerprint(apngOne)
	if err != nil {
		t.Fatal(err)
	}
	apngTwoFingerprint, err := canonicalLibTVPNGFingerprint(apngTwo)
	if err != nil {
		t.Fatal(err)
	}
	if apngOneFingerprint == apngTwoFingerprint {
		t.Fatal("changing APNG chunk data must change the canonical fingerprint")
	}
}

func TestLibTVPNGAIGCFingerprintRejectsInvalidStructure(t *testing.T) {
	payload := testPNGWithITXt(
		t,
		color.NRGBA{R: 20, G: 40, B: 60, A: 255},
		testLibTVAIGCMetadata("libtv"+strings.Repeat("a", 32)),
	)
	badCRC := append([]byte(nil), payload...)
	badCRC[len(badCRC)-1] ^= 0x01
	for name, invalid := range map[string][]byte{
		"bad CRC":  badCRC,
		"trailing": append(append([]byte(nil), payload...), 0),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := canonicalLibTVPNGFingerprint(invalid); err == nil {
				t.Fatal("canonicalLibTVPNGFingerprint() error = nil, want structural rejection")
			}
		})
	}
}

func TestImportMediaMigratesLegacyPNGCheckpointWhenOnlyITXtChanges(t *testing.T) {
	oldPNG := testPNGWithITXt(t, color.NRGBA{R: 20, G: 40, B: 60, A: 255}, testLibTVAIGCMetadata("libtv"+strings.Repeat("a", 32)))
	currentPNG := testPNGWithITXt(t, color.NRGBA{R: 20, G: 40, B: 60, A: 255}, testLibTVAIGCMetadata("libtv"+strings.Repeat("b", 32)))
	if len(oldPNG) != len(currentPNG) {
		t.Fatalf("PNG sizes = %d/%d, want metadata-only fixtures with equal size", len(oldPNG), len(currentPNG))
	}
	opts, oldSHA := testPNGCheckpointMigrationOptions(t, oldPNG, currentPNG)
	if oldSHA == opts.Media[0].SHA256 {
		t.Fatal("metadata-only PNG fixtures unexpectedly have the same raw SHA-256")
	}
	oldPath := filepath.Join(opts.BundleRoot, "export-old", "media", "one.png")
	oldFingerprint, err := importMediaContentFingerprint(oldPath, oldSHA)
	if err != nil || oldFingerprint == "" || oldFingerprint != opts.Media[0].ContentFingerprint {
		t.Fatalf("old/current fingerprints = %q/%q, error=%v, want equal normalized AIGC content", oldFingerprint, opts.Media[0].ContentFingerprint, err)
	}
	api := &fakeImportMediaAPI{}
	resolved, err := resolveImportMedia(context.Background(), opts, api, io.Discard)
	if err != nil {
		t.Fatalf("resolveImportMedia() error = %v", err)
	}
	if api.uploads != 0 || api.queries != 1 || len(resolved.Media) != 1 ||
		resolved.Media[0].PippitAssetID != "pippit-legacy" {
		t.Fatalf("uploads/queries/resolved = %d/%d/%#v, want durable legacy reuse", api.uploads, api.queries, resolved.Media)
	}
	saved := readTestMediaCheckpoint(t, opts.CheckpointPath)
	if len(saved.Entries) != 1 || saved.Entries[0].SHA256 != oldSHA ||
		saved.Entries[0].ContentFingerprint != opts.Media[0].ContentFingerprint ||
		saved.Entries[0].CanonicalByteSize != int64(len(oldPNG)) {
		t.Fatalf("checkpoint = %#v, want original uploaded identity plus normalized AIGC fingerprint", saved)
	}
	canonicalPlan, err := canonicalizeImportPlanMedia(opts.Plan, opts.Target, opts.CanvasJournalPath, opts.CheckpointPath)
	if err != nil {
		t.Fatalf("canonicalizeImportPlanMedia() error = %v", err)
	}
	if canonicalPlan.RequiredMedia[0].SHA256 != oldSHA || canonicalPlan.RequiredMedia[0].Metadata.ByteSize == nil ||
		*canonicalPlan.RequiredMedia[0].Metadata.ByteSize != int64(len(oldPNG)) {
		t.Fatalf("canonical media = %#v, want original uploaded SHA/size", canonicalPlan.RequiredMedia[0])
	}
}

func TestImportMediaRejectsLegacyPNGCheckpointWhenPixelsChange(t *testing.T) {
	oldPNG := testPNGWithITXt(t, color.NRGBA{R: 20, G: 40, B: 60, A: 255}, testLibTVAIGCMetadata("libtv"+strings.Repeat("a", 32)))
	currentPNG := testPNGWithITXt(t, color.NRGBA{R: 21, G: 40, B: 60, A: 255}, testLibTVAIGCMetadata("libtv"+strings.Repeat("b", 32)))
	opts, oldSHA := testPNGCheckpointMigrationOptions(t, oldPNG, currentPNG)
	api := &fakeImportMediaAPI{}
	_, err := resolveImportMedia(context.Background(), opts, api, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "图片内容在断点记录创建后发生了变化") {
		t.Fatalf("resolveImportMedia() error = %v, want normalized-content mismatch rejection", err)
	}
	if api.uploads != 0 || api.queries != 0 {
		t.Fatalf("uploads/queries = %d/%d, want no remote calls after pixel mismatch", api.uploads, api.queries)
	}
	saved := readTestMediaCheckpoint(t, opts.CheckpointPath)
	if len(saved.Entries) != 1 || saved.Entries[0].SHA256 != oldSHA || saved.Entries[0].ContentFingerprint != "" {
		t.Fatalf("checkpoint = %#v, want legacy entry left unchanged", saved)
	}
}

func TestImportMediaProgressReportsEmptySet(t *testing.T) {
	root := t.TempDir()
	bundleRoot := filepath.Join(root, "exports")
	opts := mediaResolutionOptions{
		Plan: canvasplan.Plan{
			Schema: canvasplan.PlanSchema,
			Source: canvasplan.Source{
				Provider: "libtv", ProjectID: "037a5c49e1b344e5adbc899ad93fdca9",
				Fingerprint: "sha256:" + strings.Repeat("1", 64),
			},
		},
		Target:            "https://xyq.jianying.com|prod",
		BundleDir:         filepath.Join(bundleRoot, "export-empty"),
		BundleRoot:        bundleRoot,
		CanvasJournalPath: filepath.Join(root, "state", "canvas.journal.json"),
		CheckpointPath:    filepath.Join(root, "state", "canvas.journal.json.media.json"),
		PollInterval:      time.Millisecond,
		WaitTimeout:       time.Second,
	}
	if err := os.MkdirAll(opts.BundleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	resolved, err := resolveImportMedia(context.Background(), opts, &fakeImportMediaAPI{}, &stderr)
	if err != nil {
		t.Fatalf("resolveImportMedia() error = %v", err)
	}
	if len(resolved.Media) != 0 || !strings.Contains(
		stderr.String(),
		`素材进度：已处理=0/0，剩余=0，状态=完成，文件="（无）"`,
	) {
		t.Fatalf("resolved/stderr = %#v/%q, want explicit 0/0 progress", resolved, stderr.String())
	}
}

func TestImportCommandBlocksUnknownUploadOutcome(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testSingleMediaPlan(t)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &fakeImportMediaAPI{uploadErr: errors.New("connection reset")}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, exporter, media, executor)
	for attempt := 0; attempt < 2; attempt++ {
		cmd := newImportCommand(io.Discard, io.Discard, deps)
		cmd.SilenceUsage = true
		cmd.SetArgs([]string{"--from", "libtv", "--url", testLibTVURL})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "blocked") {
			t.Fatalf("attempt %d error = %v, want blocked checkpoint", attempt+1, err)
		}
	}
	if media.uploads != 1 || executor.calls != 0 {
		t.Fatalf("upload/execute = %d/%d, want no blind retry or execute", media.uploads, executor.calls)
	}
}

func TestMediaCheckpointBlocksResumeAfterUploadCrashWindow(t *testing.T) {
	opts := testMediaResolutionOptions(t)
	crashing := &panickingImportMediaAPI{}
	panicked := false
	func() {
		defer func() {
			panicked = recover() != nil
		}()
		_, _ = resolveImportMedia(context.Background(), opts, crashing, io.Discard)
	}()
	if !panicked || crashing.uploads != 1 {
		t.Fatalf("panic/uploads = %v/%d, want simulated crash after one dispatch", panicked, crashing.uploads)
	}
	checkpoint := readTestMediaCheckpoint(t, opts.CheckpointPath)
	if len(checkpoint.Entries) != 1 || checkpoint.Entries[0].Status != mediaStatusUploadRequested {
		t.Fatalf("checkpoint entries = %#v, want durable upload-requested", checkpoint.Entries)
	}

	retry := &fakeImportMediaAPI{}
	_, err := resolveImportMedia(context.Background(), opts, retry, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "blocked-on-interruption") {
		t.Fatalf("resume error = %v, want blocked-on-interruption", err)
	}
	if retry.uploads != 0 {
		t.Fatalf("resume uploads = %d, want no blind re-upload", retry.uploads)
	}
	checkpoint = readTestMediaCheckpoint(t, opts.CheckpointPath)
	if checkpoint.Entries[0].Status != mediaStatusBlockedInterruption {
		t.Fatalf("checkpoint status = %q, want blocked-on-interruption", checkpoint.Entries[0].Status)
	}
}

func TestMediaCheckpointDoesNotMarkMissingAKAsUploadRequested(t *testing.T) {
	opts := testMediaResolutionOptions(t)
	api := &missingAKPreflightMediaAPI{}
	_, err := resolveImportMedia(context.Background(), opts, api, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "授权检查失败") {
		t.Fatalf("resolveImportMedia() error = %v, want explicit authentication failure", err)
	}
	if api.uploads != 0 {
		t.Fatalf("uploads = %d, want preflight rejection before dispatch", api.uploads)
	}
	checkpoint := readTestMediaCheckpoint(t, opts.CheckpointPath)
	if len(checkpoint.Entries) != 0 {
		t.Fatalf("checkpoint entries = %#v, want no ambiguous upload marker", checkpoint.Entries)
	}
}

func TestDefaultJournalPathSeparatesPippitAccessKeys(t *testing.T) {
	configDirectory := t.TempDir()
	configDir := func() (string, error) { return configDirectory, nil }
	source := canvasplan.Source{
		Provider: "libtv", ProjectID: "037a5c49e1b344e5adbc899ad93fdca9", Fingerprint: "sha256:" + strings.Repeat("1", 64),
	}
	target := "https://xyq.jianying.com|ppe_cli_canvas_ak"
	firstScope := canvasImportAuthScope(&common.Runner{Config: &config.Config{AccessKey: "first-account-ak"}})
	secondScope := canvasImportAuthScope(&common.Runner{Config: &config.Config{AccessKey: "second-account-ak"}})
	firstPath, err := resolveImportJournalPath("", source, target, firstScope, configDir)
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := resolveImportJournalPath("", source, target, secondScope, configDir)
	if err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath {
		t.Fatalf("journal paths are equal across Access Keys: %s", firstPath)
	}
	for _, secret := range []string{"first-account-ak", "second-account-ak", firstScope, secondScope} {
		if strings.Contains(firstPath, secret) || strings.Contains(secondPath, secret) {
			t.Fatalf("journal path persisted Access Key material: %q", secret)
		}
	}
}

func TestReadyMediaCheckpointMustBelongToCurrentPippitAccount(t *testing.T) {
	opts := testMediaResolutionOptions(t)
	item := opts.Media[0]
	checkpoint := &mediaCheckpoint{
		Schema:     mediaCheckpointSchema,
		Source:     opts.Plan.Source,
		Target:     opts.Target,
		BundleDirs: []string{opts.BundleDir},
		Entries: []mediaCheckpointEntry{{
			LogicalID: item.LogicalID, MediaType: item.MediaType, SHA256: item.SHA256,
			Status: mediaStatusReady, AssetID: "asset-old-account", PippitAssetID: "pippit-old-account",
		}},
	}
	if err := saveMediaCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
		t.Fatal(err)
	}
	api := &fakeImportMediaAPI{queryErr: errors.New("asset not found for current account")}
	_, err := resolveImportMedia(context.Background(), opts, api, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "当前小云雀账号") {
		t.Fatalf("resolveImportMedia() error = %v, want cross-account checkpoint rejection", err)
	}
	if api.queries != 1 || api.uploads != 0 {
		t.Fatalf("queries/uploads = %d/%d, want 1/0", api.queries, api.uploads)
	}
}

func TestMediaCheckpointLockPreventsConcurrentUpload(t *testing.T) {
	opts := testMediaResolutionOptions(t)
	first := &blockingImportMediaAPI{started: make(chan struct{}), release: make(chan struct{})}
	firstDone := make(chan error, 1)
	go func() {
		_, err := resolveImportMedia(context.Background(), opts, first, io.Discard)
		firstDone <- err
	}()
	<-first.started

	second := &fakeImportMediaAPI{}
	_, secondErr := resolveImportMedia(context.Background(), opts, second, io.Discard)
	if secondErr == nil || !strings.Contains(secondErr.Error(), "locked by another process") {
		t.Fatalf("concurrent resolve error = %v, want checkpoint lock rejection", secondErr)
	}
	if second.uploads != 0 {
		t.Fatalf("concurrent uploads = %d, want none", second.uploads)
	}
	close(first.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first resolve error = %v", err)
	}
	if first.uploads != 1 {
		t.Fatalf("first uploads = %d, want one", first.uploads)
	}
}

func TestMediaCheckpointRejectsLockSymlink(t *testing.T) {
	opts := testMediaResolutionOptions(t)
	target := filepath.Join(t.TempDir(), "lock-target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(opts.CheckpointPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, opts.CheckpointPath+".lock"); err != nil {
		t.Fatal(err)
	}
	_, err := resolveImportMedia(context.Background(), opts, &fakeImportMediaAPI{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "must not be a symbolic link") {
		t.Fatalf("resolveImportMedia() error = %v, want lock symlink rejection", err)
	}
}

func TestExporterEnvironmentUsesRuntimeAllowlist(t *testing.T) {
	got := strings.Join(sanitizedExporterEnv([]string{
		"PATH=/bin", "HOME=/user/home", "LANG=zh_CN.UTF-8", "XDG_CONFIG_HOME=/user/config",
		"LIBTV_CLI_PATH=/trusted/libtv", "PIPPIT_CLI_LIBTV_CACHE_DIR=/user/cache/libtv",
		"HTTPS_PROXY=http://proxy.example:8080", "ALL_PROXY=socks5h://proxy.example:1080", "NO_PROXY=localhost,127.0.0.1",
		"XYQ_ACCESS_KEY=secret-xyq", "PIPPIT_ACCESS_KEY=secret-pippit", "PIPPIT_CLI_PPE_ENV=ppe_lane",
		"THIRD_PARTY_API_KEY=secret-third-party", "SSH_AUTH_SOCK=/private/ssh-agent",
		"NODE_OPTIONS=--require=/private/injected.js", "PIPPIT_CLI_PACKAGE_ROOT=/pkg",
		"HTTP_PROXY=http://proxy-user:proxy-password@proxy.example:8080",
		"HTTPS_PROXY=file:///private/proxy", "HTTPS_PROXY=http://proxy.example:8080?token=secret-query",
		"HTTPS_PROXY=http://proxy.example:8080#secret-fragment", "XDG_API_TOKEN=secret-xdg",
	}), "\n")
	for _, allowed := range []string{
		"PATH=/bin", "HOME=/user/home", "LANG=zh_CN.UTF-8", "XDG_CONFIG_HOME=/user/config",
		"LIBTV_CLI_PATH=/trusted/libtv", "PIPPIT_CLI_LIBTV_CACHE_DIR=/user/cache/libtv",
		"HTTPS_PROXY=http://proxy.example:8080", "ALL_PROXY=socks5h://proxy.example:1080", "NO_PROXY=localhost,127.0.0.1",
	} {
		if !strings.Contains(got, allowed) {
			t.Fatalf("sanitized environment removed allowed runtime value %q: %s", allowed, got)
		}
	}
	for _, forbidden := range []string{
		"secret-xyq", "secret-pippit", "ppe_lane", "secret-third-party", "/private/ssh-agent",
		"--require=/private/injected.js", "PIPPIT_CLI_PACKAGE_ROOT=/pkg", "proxy-password",
		"file:///private/proxy", "secret-query", "secret-fragment", "secret-xdg",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("sanitized environment leaked forbidden value %q: %s", forbidden, got)
		}
	}
}

func TestLibTVURLRejectsAmbiguousProjectIdentity(t *testing.T) {
	_, err := normalizeLibTVURL(
		"https://www.liblib.tv/canvas?projectId=037a5c49e1b344e5adbc899ad93fdca9&projectId=11111111111111111111111111111111",
	)
	if err == nil {
		t.Fatal("normalizeLibTVURL() error = nil, want duplicate projectId rejection")
	}
}

func TestTrustedCanvasURLUsesRootAndOverviewIdentifiers(t *testing.T) {
	result := verifiedImportResult()
	if err := validateTrustedCanvasURL(result); err != nil {
		t.Fatalf("validateTrustedCanvasURL() error = %v", err)
	}
	result.WebURL = "https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=300"
	if err := validateTrustedCanvasURL(result); err == nil {
		t.Fatal("validateTrustedCanvasURL() error = nil, want overview-as-canvas rejection")
	}
	result.WebURL = "https://xyq.jianying.com:8443/novel/detail/canvas?projectId=100"
	if err := validateTrustedCanvasURL(result); err == nil {
		t.Fatal("validateTrustedCanvasURL() error = nil, want untrusted port rejection")
	}
}

const testLibTVURL = "https://www.liblib.tv/canvas?spaceId=3872811&projectId=037a5c49e1b344e5adbc899ad93fdca9"

func testImportDependencies(
	root string,
	exporter importExporter,
	media importMediaAPI,
	executor importExecutor,
) importDependencies {
	return importDependencies{
		pippitAuth:    &fakeImportAuthAPI{accessKey: "test-access-key"},
		sourceAuth:    exporter.(importSourceAuthenticator),
		exporter:      exporter,
		media:         media,
		executor:      executor,
		openURL:       func(context.Context, string) error { return nil },
		userCacheDir:  func() (string, error) { return filepath.Join(root, "cache"), nil },
		userConfigDir: func() (string, error) { return filepath.Join(root, "config"), nil },
		target:        func() string { return "https://xyq.jianying.com|ppe_cli_canvas_ak" },
		authScope:     func() string { return strings.Repeat("a", 64) },
		mediaPoll:     time.Millisecond,
		mediaTimeout:  time.Second,
	}
}

func prepareSourceBoundResumeFixture(
	t *testing.T,
	root string,
) (canvasplan.Plan, *fakeImportExporter, string) {
	t.Helper()
	plan, _ := testImportPlan(t, false)
	plan.RequiredMedia = nil
	plan.Nodes = []canvasplan.Node{{
		LogicalID: "node:placeholder", SourceNodeID: "placeholder", Title: "Pending image",
		Position: canvasplan.Position{X: 0, Y: 0}, Size: canvasplan.Size{Width: 100, Height: 100},
		Kind: "image-placeholder", TargetType: "biz/image",
	}}
	plan.Degradations = []json.RawMessage{json.RawMessage(`{"code":"test.placeholder"}`)}
	exporter := &fakeImportExporter{plan: plan, mediaBytes: map[string][]byte{}}
	journalPath := filepath.Join(root, "existing.journal.json")
	if err := os.WriteFile(journalPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint := &mediaCheckpoint{
		Schema:  mediaCheckpointSchema,
		Source:  plan.Source,
		Target:  "https://xyq.jianying.com|ppe_cli_canvas_ak",
		Entries: []mediaCheckpointEntry{},
	}
	if err := saveMediaCheckpoint(journalPath+".media.json", checkpoint); err != nil {
		t.Fatal(err)
	}
	return plan, exporter, journalPath
}

func testImportPlan(t *testing.T, degraded bool) (canvasplan.Plan, map[string][]byte) {
	t.Helper()
	payload := []byte("same-image-bytes")
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	size := int64(len(payload))
	plan := canvasplan.Plan{
		Schema: canvasplan.PlanSchema,
		Title:  "Imported LibTV canvas",
		Source: canvasplan.Source{Provider: "libtv", ProjectID: "037a5c49e1b344e5adbc899ad93fdca9", Fingerprint: "sha256:" + strings.Repeat("1", 64)},
		RequiredMedia: []canvasplan.MediaRequirement{
			{LogicalID: "media:image-1", SourceNodeID: "image-1", FileName: "one.png", MediaType: "image", LocalPath: "media/one.png", SHA256: hash, Metadata: canvasplan.MediaMetadata{ByteSize: &size}},
			{LogicalID: "media:image-2", SourceNodeID: "image-2", FileName: "two.png", MediaType: "image", LocalPath: "media/two.png", SHA256: hash, Metadata: canvasplan.MediaMetadata{ByteSize: &size}},
		},
		Nodes: []canvasplan.Node{
			{LogicalID: "node:image-1", SourceNodeID: "image-1", Title: "One", Position: canvasplan.Position{X: 0, Y: 0}, Size: canvasplan.Size{Width: 100, Height: 100}, Kind: "image", TargetType: "biz/image", MediaLogicalID: "media:image-1"},
			{LogicalID: "node:image-2", SourceNodeID: "image-2", Title: "Two", Position: canvasplan.Position{X: 120, Y: 0}, Size: canvasplan.Size{Width: 100, Height: 100}, Kind: "image", TargetType: "biz/image", MediaLogicalID: "media:image-2"},
		},
		Groups: []canvasplan.Group{},
		Edges:  []canvasplan.Edge{},
	}
	if degraded {
		plan.Degradations = []json.RawMessage{json.RawMessage(`{"code":"test.degradation"}`)}
	}
	return plan, map[string][]byte{"media/one.png": payload, "media/two.png": payload}
}

func testSingleMediaPlan(t *testing.T) (canvasplan.Plan, map[string][]byte) {
	plan, media := testImportPlan(t, false)
	plan.RequiredMedia = plan.RequiredMedia[:1]
	plan.Nodes = plan.Nodes[:1]
	delete(media, "media/two.png")
	return plan, media
}

func testMediaResolutionOptions(t *testing.T) mediaResolutionOptions {
	t.Helper()
	root := t.TempDir()
	plan, mediaBytes := testSingleMediaPlan(t)
	bundleRoot := filepath.Join(root, "cache", "exports")
	bundleDir := filepath.Join(bundleRoot, "export-fixture")
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	if _, err := exporter.Export(context.Background(), testLibTVURL, bundleDir, io.Discard); err != nil {
		t.Fatalf("prepare export fixture: %v", err)
	}
	media, err := readAndValidateExportMedia(bundleDir, plan)
	if err != nil {
		t.Fatalf("validate export fixture media: %v", err)
	}
	return mediaResolutionOptions{
		Plan:              plan,
		Media:             media,
		Target:            "https://xyq.jianying.com|ppe_cli_canvas_ak",
		BundleDir:         bundleDir,
		BundleRoot:        bundleRoot,
		CanvasJournalPath: filepath.Join(root, "state", "canvas.journal.json"),
		CheckpointPath:    filepath.Join(root, "state", "canvas.journal.json.media.json"),
		PollInterval:      time.Millisecond,
		WaitTimeout:       time.Second,
	}
}

func testPNGCheckpointMigrationOptions(t *testing.T, oldPNG, currentPNG []byte) (mediaResolutionOptions, string) {
	t.Helper()
	root := t.TempDir()
	bundleRoot := filepath.Join(root, "cache", "exports")
	oldBundle := filepath.Join(bundleRoot, "export-old")
	currentBundle := filepath.Join(bundleRoot, "export-current")
	oldPlan, oldMedia := testSingleMediaPlan(t)
	currentPlan := oldPlan
	currentPlan.RequiredMedia = append([]canvasplan.MediaRequirement(nil), oldPlan.RequiredMedia...)
	oldSize := int64(len(oldPNG))
	oldDigest := sha256.Sum256(oldPNG)
	oldSHA := hex.EncodeToString(oldDigest[:])
	oldPlan.RequiredMedia[0].SHA256 = oldSHA
	oldPlan.RequiredMedia[0].Metadata.ByteSize = &oldSize
	oldMedia["media/one.png"] = oldPNG
	currentSize := int64(len(currentPNG))
	currentDigest := sha256.Sum256(currentPNG)
	currentPlan.RequiredMedia[0].SHA256 = hex.EncodeToString(currentDigest[:])
	currentPlan.RequiredMedia[0].Metadata.ByteSize = &currentSize
	currentMedia := map[string][]byte{"media/one.png": currentPNG}
	oldExporter := &fakeImportExporter{plan: oldPlan, mediaBytes: oldMedia}
	if _, err := oldExporter.Export(context.Background(), testLibTVURL, oldBundle, io.Discard); err != nil {
		t.Fatalf("prepare old PNG export: %v", err)
	}
	currentExporter := &fakeImportExporter{plan: currentPlan, mediaBytes: currentMedia}
	if _, err := currentExporter.Export(context.Background(), testLibTVURL, currentBundle, io.Discard); err != nil {
		t.Fatalf("prepare current PNG export: %v", err)
	}
	media, err := readAndValidateExportMedia(currentBundle, currentPlan)
	if err != nil {
		t.Fatalf("validate current PNG export: %v", err)
	}
	opts := mediaResolutionOptions{
		Plan:              currentPlan,
		Media:             media,
		Target:            "https://xyq.jianying.com|ppe_cli_canvas_ak",
		BundleDir:         currentBundle,
		BundleRoot:        bundleRoot,
		CanvasJournalPath: filepath.Join(root, "state", "canvas.journal.json"),
		CheckpointPath:    filepath.Join(root, "state", "canvas.journal.json.media.json"),
		PollInterval:      time.Millisecond,
		WaitTimeout:       time.Second,
	}
	checkpoint := &mediaCheckpoint{
		Schema:     mediaCheckpointSchema,
		Source:     currentPlan.Source,
		Target:     opts.Target,
		BundleDirs: []string{oldBundle},
		Entries: []mediaCheckpointEntry{{
			LogicalID: "media:image-1", MediaType: "image", SHA256: oldSHA,
			Status: mediaStatusReady, AssetID: "asset-legacy", PippitAssetID: "pippit-legacy",
		}},
	}
	if err := saveMediaCheckpoint(opts.CheckpointPath, checkpoint); err != nil {
		t.Fatalf("save legacy PNG checkpoint: %v", err)
	}
	return opts, oldSHA
}

func testPNGWithITXt(t *testing.T, pixel color.NRGBA, metadata string) []byte {
	t.Helper()
	imageValue := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			imageValue.SetNRGBA(x, y, pixel)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, imageValue); err != nil {
		t.Fatalf("encode PNG fixture: %v", err)
	}
	data := append([]byte("AIGC\x00\x00\x00\x00\x00"), []byte(metadata)...)
	return testInsertPNGChunkBeforeIEND(t, encoded.Bytes(), "iTXt", data)
}

func testLibTVAIGCMetadata(id string) string {
	return fmt.Sprintf(
		`{"Label":"1","ContentProducer":%q,"ProduceID":%q,"ReservedCode1":"","ContentPropagator":%q,"PropagateID":%q,"ReservedCode2":""}`,
		libTVAIGCProducer,
		id,
		libTVAIGCProducer,
		id,
	)
}

func testInsertPNGChunkBeforeIEND(t *testing.T, payload []byte, chunkType string, data []byte) []byte {
	t.Helper()
	if len(chunkType) != 4 {
		t.Fatalf("PNG chunk type %q must have four bytes", chunkType)
	}
	if len(payload) < 12 || string(payload[len(payload)-8:len(payload)-4]) != "IEND" {
		t.Fatal("PNG fixture has no terminal IEND chunk")
	}
	chunk := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(data)))
	copy(chunk[4:8], chunkType)
	copy(chunk[8:8+len(data)], data)
	checksumInput := append([]byte(chunkType), data...)
	binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(checksumInput))
	result := make([]byte, 0, len(payload)+len(chunk))
	result = append(result, payload[:len(payload)-12]...)
	result = append(result, chunk...)
	result = append(result, payload[len(payload)-12:]...)
	return result
}

func testRewriteFirstPNGChunk(t *testing.T, payload []byte, expectedType string, rewrite func([]byte)) []byte {
	t.Helper()
	result := append([]byte(nil), payload...)
	for offset := len(pngFileSignature); offset+12 <= len(result); {
		length := int(binary.BigEndian.Uint32(result[offset : offset+4]))
		end := offset + 12 + length
		if end > len(result) {
			t.Fatal("PNG fixture has a truncated chunk")
		}
		if string(result[offset+4:offset+8]) == expectedType {
			data := result[offset+8 : offset+8+length]
			if len(data) == 0 {
				t.Fatalf("PNG %s fixture chunk has no data", expectedType)
			}
			rewrite(data)
			checksumInput := append([]byte(expectedType), data...)
			binary.BigEndian.PutUint32(result[offset+8+length:end], crc32.ChecksumIEEE(checksumInput))
			return result
		}
		offset = end
	}
	t.Fatalf("PNG fixture has no %s chunk", expectedType)
	return nil
}

func readTestMediaCheckpoint(t *testing.T, path string) mediaCheckpoint {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read media checkpoint: %v", err)
	}
	var checkpoint mediaCheckpoint
	if err := json.Unmarshal(payload, &checkpoint); err != nil {
		t.Fatalf("decode media checkpoint: %v", err)
	}
	return checkpoint
}

func verifiedImportResult() *canvasplan.ExecutionResult {
	return &canvasplan.ExecutionResult{
		State:                 canvasplan.StateVerified,
		ProjectID:             "100",
		RootCanvasID:          "200",
		OverviewPippitAssetID: "300",
		WebURL:                "https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=200&overviewPippitAssetId=300",
		Verification:          &canvasplan.Verification{Verified: true},
	}
}

func writeTestJSON(path string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}
