package canvas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
}

func (api *fakeImportMediaAPI) Upload(_ context.Context, _ string) (*canvascore.UploadResult, error) {
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

func (api *fakeImportMediaAPI) Query(context.Context, string) error {
	api.queries++
	return api.queryErr
}

type panickingImportMediaAPI struct {
	uploads int
}

func (api *panickingImportMediaAPI) Upload(context.Context, string) (*canvascore.UploadResult, error) {
	api.uploads++
	panic("simulated process exit during upload")
}

func (*panickingImportMediaAPI) Query(context.Context, string) error {
	return nil
}

type missingAKPreflightMediaAPI struct {
	uploads int
}

func (*missingAKPreflightMediaAPI) PreflightUpload(context.Context) error {
	return errors.New("XYQ_ACCESS_KEY 缺失")
}

func (api *missingAKPreflightMediaAPI) Upload(context.Context, string) (*canvascore.UploadResult, error) {
	api.uploads++
	return nil, errors.New("must not be called")
}

func (*missingAKPreflightMediaAPI) Query(context.Context, string) error {
	return nil
}

type blockingImportMediaAPI struct {
	started chan struct{}
	release chan struct{}
	uploads int
}

func (api *blockingImportMediaAPI) Upload(context.Context, string) (*canvascore.UploadResult, error) {
	api.uploads++
	close(api.started)
	<-api.release
	return &canvascore.UploadResult{
		State: canvascore.StateReady, AssetID: "asset-blocking", PippitAssetID: "pippit-blocking",
	}, nil
}

func (*blockingImportMediaAPI) Query(context.Context, string) error {
	return nil
}

type fakeImportExecutor struct {
	calls    int
	resolved canvasplan.ResolvedMediaSet
	opts     canvasplan.ExecuteOptions
	result   *canvasplan.ExecutionResult
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
		"Source provider [libtv]", "LibTV canvas URL", "Resume journal path [automatic]",
		"Open the imported Canvas when finished? [Y/n]",
		"Resume journal: " + executor.opts.JournalPath,
		`Media progress: processed=1/2 remaining=1 action=uploaded file="one.png"`,
		`Media progress: processed=2/2 remaining=0 action=reused file="two.png"`,
		`Media progress: processed=0/2 remaining=2 action=uploading file="one.png"`,
		"Phase canvas: create/resume, materialize, apply, then verify remote Canvas assets.",
		"Phase canvas: Canvas import verified by query-back.",
	} {
		if !strings.Contains(stderr.String(), message) {
			t.Fatalf("stderr missing %q:\n%s", message, stderr.String())
		}
	}
	if strings.Count(stdout.String(), "\n") != 1 || !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("stdout = %q, want one final JSON line", stdout.String())
	}
}

func TestImportCommandInteractiveWizardCanAcceptDegradations(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testImportPlan(t, true)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, exporter, &fakeImportMediaAPI{}, executor)
	deps.isInteractive = func(io.Reader) bool { return true }
	var stdout, stderr bytes.Buffer
	cmd := newImportCommand(&stdout, &stderr, deps)
	cmd.SetIn(strings.NewReader("\n" + testLibTVURL + "\n\nn\ny\n"))
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Continue importing with these degradations? [y/N]") {
		t.Fatalf("stderr = %q, want in-session degradation confirmation", stderr.String())
	}
	if executor.calls != 1 || !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("executor/stdout = %d/%q, want completed interactive import", executor.calls, stdout.String())
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
	if err == nil || !strings.Contains(err.Error(), "stdin is not interactive") ||
		!strings.Contains(err.Error(), "--from libtv --url") {
		t.Fatalf("Execute() error = %v, want actionable non-interactive flags", err)
	}
	if len(exporter.urls) != 0 {
		t.Fatalf("exporter called before required input validation: %#v", exporter.urls)
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
	if err == nil || !strings.Contains(err.Error(), "ended before a LibTV URL") ||
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

func TestImportCommandCheckpointsProcessingUploadAndDoesNotUploadAgain(t *testing.T) {
	temp := t.TempDir()
	plan, mediaBytes := testSingleMediaPlan(t)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &fakeImportMediaAPI{uploadState: canvascore.StateProcessing}
	executor := &fakeImportExecutor{result: verifiedImportResult()}
	deps := testImportDependencies(temp, exporter, media, executor)
	var stdout, stderr bytes.Buffer
	first := newImportCommand(&stdout, &stderr, deps)
	first.SilenceUsage = true
	first.SetArgs([]string{"--from", "libtv", "--url", testLibTVURL})
	if err := first.Execute(); err == nil || !strings.Contains(err.Error(), "still processing") {
		t.Fatalf("first Execute() error = %v, want processing checkpoint", err)
	}
	media.uploadState = canvascore.StateReady
	stdout.Reset()
	stderr.Reset()
	second := newImportCommand(&stdout, &stderr, deps)
	second.SetArgs([]string{"--from", "libtv", "--url", testLibTVURL})
	if err := second.Execute(); err != nil {
		t.Fatalf("second Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if media.uploads != 1 || media.queries != 1 || executor.calls != 1 {
		t.Fatalf("upload/query/execute = %d/%d/%d, want 1/1/1", media.uploads, media.queries, executor.calls)
	}
	if !strings.Contains(stderr.String(), `Media progress: processed=1/1 remaining=0 action=queried file="one.png"`) {
		t.Fatalf("stderr = %q, want stable query progress", stderr.String())
	}
	if !strings.Contains(stderr.String(), `Media progress: processed=0/1 remaining=1 action=checking file="one.png"`) {
		t.Fatalf("stderr = %q, want pre-query progress before a potentially slow request", stderr.String())
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
		`Media progress: processed=0/0 remaining=0 action=complete file="(none)"`,
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
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
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
	if err == nil || !strings.Contains(err.Error(), "current Pippit account") {
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
		exporter:      exporter,
		media:         media,
		executor:      executor,
		openURL:       func(context.Context, string) error { return nil },
		userCacheDir:  func() (string, error) { return filepath.Join(root, "cache"), nil },
		userConfigDir: func() (string, error) { return filepath.Join(root, "config"), nil },
		target:        func() string { return "https://xyq.jianying.com|ppe_cli_canvas_ak" },
		authScope:     func() string { return strings.Repeat("a", 64) },
	}
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
	}
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
