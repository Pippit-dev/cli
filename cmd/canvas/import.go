package canvas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

type importOptions struct {
	Provider                   string
	SourceURL                  string
	Open                       bool
	AcceptDegradations         bool
	JournalPath                string
	OpenExplicit               bool
	AcceptDegradationsExplicit bool
	JournalExplicit            bool
}

var (
	libTVProjectIDPattern = regexp.MustCompile(`^(?:[0-9a-fA-F]{32}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)
	libTVSpaceIDPattern   = regexp.MustCompile(`^[0-9]+$`)
)

type importExporter interface {
	Export(context.Context, string, string, io.Writer) (*libTVExportResult, error)
}

type importExecutor interface {
	Execute(context.Context, canvasplan.Plan, canvasplan.ResolvedMediaSet, canvasplan.ExecuteOptions) (*canvasplan.ExecutionResult, error)
}

type importDependencies struct {
	exporter      importExporter
	media         importMediaAPI
	executor      importExecutor
	openURL       func(context.Context, string) error
	userCacheDir  func() (string, error)
	userConfigDir func() (string, error)
	target        func() string
	authScope     func() string
	isInteractive func(io.Reader) bool
}

type runnerImportExecutor struct {
	executor *canvasplan.Executor
}

func (executor runnerImportExecutor) Execute(
	ctx context.Context,
	plan canvasplan.Plan,
	resolved canvasplan.ResolvedMediaSet,
	opts canvasplan.ExecuteOptions,
) (*canvasplan.ExecutionResult, error) {
	return executor.executor.Execute(ctx, plan, resolved, opts)
}

func newImportDependencies(runner *common.Runner) importDependencies {
	return importDependencies{
		exporter:      nodeLibTVExporter{},
		media:         runnerImportMediaAPI{runner: runner},
		executor:      runnerImportExecutor{executor: canvasplan.NewExecutor(runner)},
		openURL:       openBrowserURL,
		userCacheDir:  os.UserCacheDir,
		userConfigDir: os.UserConfigDir,
		target:        func() string { return canvasImportTarget(runner) },
		authScope:     func() string { return canvasImportAuthScope(runner) },
		isInteractive: importInputIsInteractive,
	}
}

func newImportCommand(
	stdout, stderr io.Writer,
	dependencies importDependencies,
) *cobra.Command {
	var opts importOptions
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an external project into a personal novel Canvas",
		Long: "Import an external project into a personal novel Canvas. " +
			"Run without source flags for a guided import; flags remain available for Agent and CI automation.",
		Example: "  pippit-tool-cli --ppe-env ppe_cli_canvas_ak canvas import",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.OpenExplicit = cmd.Flags().Changed("open")
			opts.AcceptDegradationsExplicit = cmd.Flags().Changed("accept-degradations")
			opts.JournalExplicit = cmd.Flags().Changed("journal")
			prepared, prompts, err := prepareCanvasImportOptions(
				cmd.InOrStdin(), opts, dependencies.isInteractive, stderr,
			)
			if err != nil {
				return err
			}
			result, err := runCanvasImport(cmd.Context(), prepared, dependencies, stderr, prompts)
			if result != nil {
				if writeErr := common.WriteJSON(stdout, result); writeErr != nil {
					return writeErr
				}
			}
			if err != nil {
				logCanvasError("canvas import", err, map[string]string{
					"provider": strings.TrimSpace(prepared.Provider),
					"journal":  filepath.Base(strings.TrimSpace(prepared.JournalPath)),
				})
				return err
			}
			return nil
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&opts.Provider, "from", "", "source provider (currently: libtv)")
	flags.StringVar(&opts.SourceURL, "url", "", "source project URL")
	flags.BoolVar(&opts.Open, "open", false, "open the verified personal novel Canvas")
	flags.BoolVar(&opts.AcceptDegradations, "accept-degradations", false, "accept explicitly reported source conversion degradations")
	flags.StringVar(&opts.JournalPath, "journal", "", "durable resume journal path (generated when omitted)")
	return cmd
}

func runCanvasImport(
	ctx context.Context,
	opts importOptions,
	dependencies importDependencies,
	stderr io.Writer,
	prompts *importPromptSession,
) (*canvasplan.ExecutionResult, error) {
	if strings.ToLower(strings.TrimSpace(opts.Provider)) != "libtv" {
		return nil, fmt.Errorf("canvas import --from must be libtv")
	}
	sourceURL, err := normalizeLibTVURL(opts.SourceURL)
	if err != nil {
		return nil, err
	}
	explicitJournal, err := preflightExplicitImportJournal(opts.JournalPath, opts.JournalExplicit)
	if err != nil {
		return nil, err
	}
	if explicitJournal != "" {
		opts.JournalPath = explicitJournal
	}
	bundleRoot, outputDir, err := newImportBundlePath(dependencies.userCacheDir)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(stderr, "Phase export: exporting the LibTV canvas and its media...")
	exported, err := dependencies.exporter.Export(ctx, sourceURL, outputDir, stderr)
	if err != nil {
		return nil, fmt.Errorf("export LibTV canvas: %w", err)
	}
	if err := validateExportLocation(exported, outputDir); err != nil {
		_ = removeOwnedBundle(outputDir, bundleRoot)
		return nil, err
	}
	plan, err := readCanvasPlan(exported.PlanPath)
	if err != nil {
		_ = removeOwnedBundle(outputDir, bundleRoot)
		return nil, err
	}
	if err := validateExportPlan(*exported, plan); err != nil {
		_ = removeOwnedBundle(outputDir, bundleRoot)
		return nil, err
	}
	if len(plan.Degradations) > 0 && !opts.AcceptDegradations {
		if prompts == nil || opts.AcceptDegradationsExplicit {
			return nil, fmt.Errorf(
				"LibTV export reports %d explicit degradation(s); inspect %s (plan: %s), then rerun with --accept-degradations",
				len(plan.Degradations), outputDir, exported.PlanPath,
			)
		}
		accepted, promptErr := prompts.confirmDegradations(len(plan.Degradations))
		if promptErr != nil {
			return nil, promptErr
		}
		if !accepted {
			return nil, fmt.Errorf(
				"LibTV import was cancelled because the export contains %d degradation(s); inspect %s (plan: %s)",
				len(plan.Degradations), outputDir, exported.PlanPath,
			)
		}
	}
	media, err := readAndValidateExportMedia(exported.BundleDir, plan)
	if err != nil {
		_ = removeOwnedBundle(outputDir, bundleRoot)
		return nil, err
	}
	target := dependencies.target()
	authScope := ""
	if dependencies.authScope != nil {
		authScope = dependencies.authScope()
	}
	journalPath, err := resolveImportJournalPath(
		opts.JournalPath,
		plan.Source,
		target,
		authScope,
		dependencies.userConfigDir,
	)
	if err != nil {
		_ = removeOwnedBundle(outputDir, bundleRoot)
		return nil, err
	}
	if err := canvasplan.PreflightJournalPath(journalPath); err != nil {
		_ = removeOwnedBundle(outputDir, bundleRoot)
		return nil, fmt.Errorf("preflight resolved Canvas import journal before media upload: %w", err)
	}
	fmt.Fprintf(stderr, "Resume journal: %s\n", journalPath)
	checkpointPath := journalPath + ".media.json"
	fmt.Fprintf(stderr, "Phase media: resolving %d exported media file(s)...\n", len(media))
	resolved, err := resolveImportMedia(ctx, mediaResolutionOptions{
		Plan:              plan,
		Media:             media,
		Target:            target,
		BundleDir:         outputDir,
		BundleRoot:        bundleRoot,
		CanvasJournalPath: journalPath,
		CheckpointPath:    checkpointPath,
	}, dependencies.media, stderr)
	if err != nil {
		return nil, err
	}
	fmt.Fprintln(stderr, "Phase canvas: create/resume, materialize, apply, then verify remote Canvas assets.")
	result, executeErr := dependencies.executor.Execute(ctx, plan, resolved, canvasplan.ExecuteOptions{
		JournalPath: journalPath,
	})
	if executeErr != nil {
		return result, fmt.Errorf("execute CanvasPlan: %w", executeErr)
	}
	if !verifiedExecution(result) {
		return result, fmt.Errorf("CanvasPlan execution completed without query-back verification")
	}
	if opts.Open {
		if err := validateTrustedCanvasURL(result); err != nil {
			return result, err
		}
		if err := dependencies.openURL(ctx, result.WebURL); err != nil {
			fmt.Fprintf(stderr, "Canvas verified, but could not open the browser: %v\n", err)
		}
	}
	fmt.Fprintln(stderr, "Phase canvas: Canvas import verified by query-back.")
	return result, nil
}

func normalizeLibTVURL(value string) (string, error) {
	raw := strings.TrimSpace(value)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return "", fmt.Errorf("canvas import --url must be an HTTPS LibTV canvas URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if (host != "www.liblib.tv" && host != "liblib.tv") || (parsed.Port() != "" && parsed.Port() != "443") {
		return "", fmt.Errorf("canvas import --url host must be www.liblib.tv")
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != "/canvas" {
		return "", fmt.Errorf("canvas import --url must identify a LibTV /canvas project with projectId")
	}
	if parsed.Fragment != "" {
		return "", fmt.Errorf("canvas import --url must not contain a fragment")
	}
	query := parsed.Query()
	projectIDs := query["projectId"]
	if len(projectIDs) != 1 || !libTVProjectIDPattern.MatchString(strings.TrimSpace(projectIDs[0])) {
		return "", fmt.Errorf("canvas import --url projectId must be a LibTV project UUID")
	}
	canonical := &url.URL{Scheme: "https", Host: "www.liblib.tv", Path: "/canvas"}
	canonicalQuery := url.Values{}
	canonicalQuery.Set("projectId", strings.ToLower(strings.TrimSpace(projectIDs[0])))
	if spaceIDs, exists := query["spaceId"]; exists {
		if len(spaceIDs) != 1 || !libTVSpaceIDPattern.MatchString(strings.TrimSpace(spaceIDs[0])) {
			return "", fmt.Errorf("canvas import --url spaceId must be numeric")
		}
		canonicalQuery.Set("spaceId", strings.TrimSpace(spaceIDs[0]))
	}
	canonical.RawQuery = canonicalQuery.Encode()
	return canonical.String(), nil
}

func resolveImportJournalPath(
	explicit string,
	source canvasplan.Source,
	target string,
	authScope string,
	userConfigDir func() (string, error),
) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		absolute, err := filepath.Abs(value)
		if err != nil {
			return "", fmt.Errorf("resolve canvas import journal: %w", err)
		}
		return filepath.Clean(absolute), nil
	}
	configDir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve canvas import config directory: %w", err)
	}
	directory := filepath.Join(configDir, "pippit-cli", "canvas-import")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create canvas import journal directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("secure canvas import journal directory: %w", err)
	}
	hash := sha256.Sum256([]byte(strings.Join([]string{
		target,
		authScope,
		source.Provider,
		source.ProjectID,
		source.Fingerprint,
	}, "\n")))
	return filepath.Join(directory, hex.EncodeToString(hash[:])+".journal.json"), nil
}

func canvasImportAuthScope(runner *common.Runner) string {
	accessKey := ""
	if runner != nil && runner.Config != nil {
		accessKey = strings.TrimSpace(runner.Config.AccessKey)
	}
	hash := sha256.Sum256([]byte(accessKey))
	return hex.EncodeToString(hash[:])
}

func canvasImportTarget(runner *common.Runner) string {
	if runner == nil || runner.Config == nil {
		return "unknown"
	}
	lane := strings.TrimSpace(runner.Config.PPEEnv)
	if lane == "" {
		lane = "prod"
	}
	return strings.TrimRight(strings.TrimSpace(runner.Config.BaseURL), "/") + "|" + lane
}

func verifiedExecution(result *canvasplan.ExecutionResult) bool {
	return result != nil && result.State == canvasplan.StateVerified &&
		result.Verification != nil && result.Verification.Verified
}

func validateTrustedCanvasURL(result *canvasplan.ExecutionResult) error {
	if !verifiedExecution(result) {
		return fmt.Errorf("refusing to open an unverified Canvas result")
	}
	parsed, err := url.Parse(strings.TrimSpace(result.WebURL))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil ||
		strings.ToLower(parsed.Hostname()) != "xyq.jianying.com" ||
		(parsed.Port() != "" && parsed.Port() != "443") || parsed.Fragment != "" {
		return fmt.Errorf("refusing to open untrusted Canvas URL")
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != "/novel/detail/canvas" {
		return fmt.Errorf("refusing to open a non-novel Canvas URL")
	}
	query := parsed.Query()
	if len(query["projectId"]) != 1 || query.Get("projectId") != result.ProjectID {
		return fmt.Errorf("refusing to open a Canvas URL whose project ID does not match the verified result")
	}
	if canvasIDs := query["canvasId"]; len(canvasIDs) > 1 ||
		(len(canvasIDs) == 1 && canvasIDs[0] != result.RootCanvasID) {
		return fmt.Errorf("refusing to open a Canvas URL whose canvas ID does not match the verified result")
	}
	for _, key := range []string{"overviewPippitAssetId", "overview_pippit_asset_id"} {
		if overviewIDs := query[key]; len(overviewIDs) > 1 ||
			(len(overviewIDs) == 1 && overviewIDs[0] != result.OverviewPippitAssetID) {
			return fmt.Errorf("refusing to open a Canvas URL whose overview ID does not match the verified result")
		}
	}
	return nil
}

func openBrowserURL(ctx context.Context, value string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "open", value)
	case "windows":
		command = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", value)
	default:
		command = exec.CommandContext(ctx, "xdg-open", value)
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("open Canvas URL: %w", err)
	}
	return nil
}
