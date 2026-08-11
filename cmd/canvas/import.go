package canvas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

type importOptions struct {
	Provider                   string
	SourceURL                  string
	Open                       bool
	AcceptDegradations         bool
	AcceptDegradationsExplicit bool
	JournalPath                string
	OpenExplicit               bool
	JournalExplicit            bool
}

var (
	libTVProjectIDPattern = regexp.MustCompile(`^(?:[0-9a-fA-F]{32}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)
	libTVSpaceIDPattern   = regexp.MustCompile(`^[0-9]+$`)
)

type importExporter interface {
	Export(context.Context, string, string, io.Writer) (*libTVExportResult, error)
}

type importSourceAuthenticator interface {
	Authenticate(context.Context, bool, io.Writer) error
}

type importExecutor interface {
	Execute(context.Context, canvasplan.Plan, canvasplan.ResolvedMediaSet, canvasplan.ExecuteOptions) (*canvasplan.ExecutionResult, error)
	Reconcile(context.Context, string, canvasplan.Plan, canvasplan.ResolvedMediaSet) (*canvasplan.ExecutionResult, error)
}

type importDependencies struct {
	pippitAuth    importAuthAPI
	sourceAuth    importSourceAuthenticator
	exporter      importExporter
	media         importMediaAPI
	executor      importExecutor
	openURL       func(context.Context, string) error
	userCacheDir  func() (string, error)
	userConfigDir func() (string, error)
	target        func() string
	authScope     func(context.Context) (string, error)
	isInteractive func(io.Reader) bool
	mediaPoll     time.Duration
	mediaTimeout  time.Duration
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

func (executor runnerImportExecutor) Reconcile(
	ctx context.Context,
	journalPath string,
	plan canvasplan.Plan,
	resolved canvasplan.ResolvedMediaSet,
) (*canvasplan.ExecutionResult, error) {
	return executor.executor.ReconcileWithInputs(ctx, journalPath, plan, resolved)
}

func newImportDependencies(runner *common.Runner) importDependencies {
	libTV := nodeLibTVExporter{}
	return importDependencies{
		pippitAuth:    runnerImportAuthAPI{runner: runner},
		sourceAuth:    libTV,
		exporter:      libTV,
		media:         runnerImportMediaAPI{runner: runner},
		executor:      runnerImportExecutor{executor: canvasplan.NewExecutor(runner)},
		openURL:       openBrowserURL,
		userCacheDir:  os.UserCacheDir,
		userConfigDir: os.UserConfigDir,
		target:        func() string { return canvasImportTarget(runner) },
		authScope: func(ctx context.Context) (string, error) {
			return runnerImportAuthAPI{runner: runner}.CredentialScope(ctx)
		},
		isInteractive: importInputIsInteractive,
		mediaPoll:     defaultImportMediaPollInterval,
		mediaTimeout:  defaultImportMediaWaitTimeout,
	}
}

func newImportCommand(
	stdout, stderr io.Writer,
	dependencies importDependencies,
) *cobra.Command {
	var opts importOptions
	cmd := &cobra.Command{
		Use:   "import",
		Short: "将外部项目导入个人漫剧画布",
		Long: "将外部项目导入个人漫剧画布。" +
			"不传来源参数时会进入交互式向导；Agent 和 CI 自动化仍可使用完整参数。",
		Example: "  pippit-tool-cli --ppe-env ppe_cli_canvas_ak canvas import",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.OpenExplicit = cmd.Flags().Changed("open")
			opts.JournalExplicit = cmd.Flags().Changed("journal")
			opts.AcceptDegradationsExplicit = cmd.Flags().Changed("accept-degradations")
			prepared, prompts, err := prepareCanvasImportOptions(
				cmd.Context(), cmd.InOrStdin(), opts, dependencies.isInteractive, stderr,
			)
			if err != nil {
				return err
			}
			result, err := runCanvasImport(cmd.Context(), prepared, dependencies, stderr, prompts)
			if result != nil {
				localizeCanvasImportResult(result)
				if writeErr := common.WriteJSON(stdout, result); writeErr != nil {
					return writeErr
				}
			}
			if err != nil {
				err = redactCanvasImportFinalError(err, dependencies.pippitAuth)
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
	flags.StringVar(&opts.Provider, "from", "", "导入来源（当前仅支持 libtv）")
	flags.StringVar(&opts.SourceURL, "url", "", "来源项目链接")
	flags.BoolVar(&opts.Open, "open", false, "导入完成后打开已验证的个人漫剧画布")
	flags.BoolVar(&opts.AcceptDegradations, "accept-degradations", false, "接受来源转换中明确报告的能力降级")
	flags.StringVar(&opts.JournalPath, "journal", "", "断点续跑记录路径（省略时自动生成）")
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
		return nil, fmt.Errorf("canvas import 的 --from 目前必须为 libtv")
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
	if err := preflightCanvasImportAuth(ctx, dependencies, prompts, stderr); err != nil {
		return nil, err
	}
	// Bind the whole durable import to the authenticated UID and this device
	// before exporting or creating checkpoints. Any mid-run browser login must
	// return to this exact scope or the operation stops without reusing state.
	authScope := ""
	if dependencies.authScope != nil {
		authScope, err = dependencies.authScope(ctx)
		if err != nil {
			return nil, fmt.Errorf("确定小云雀登录账号的断点作用域失败：%w", err)
		}
	}
	if strings.TrimSpace(authScope) == "" {
		return nil, fmt.Errorf("小云雀登录账号缺少可验证的断点作用域")
	}
	bundleRoot, outputDir, exported, err := exportLibTVCanvasWithRetry(
		ctx, sourceURL, dependencies, prompts, stderr,
	)
	if err != nil {
		return nil, err
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
	if len(plan.Degradations) > 0 {
		if opts.AcceptDegradations {
			// The caller explicitly accepted the adapter's auditable warnings.
		} else if prompts != nil && !opts.AcceptDegradationsExplicit {
			fmt.Fprintf(
				stderr,
				"提示：LibTV 导出结果包含 %d 项已知的非致命能力降级，例如空素材占位或语义降级；交互式导入将自动继续，最终 JSON 会记录 degradation_count。\n",
				len(plan.Degradations),
			)
		} else {
			return nil, fmt.Errorf(
				"LibTV 导出结果包含 %d 项明确的能力降级；请检查 %s（计划文件：%s），确认后使用 --accept-degradations 重新运行",
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
		return nil, fmt.Errorf("上传素材前检查画布导入断点记录失败：%w", err)
	}
	fmt.Fprintf(stderr, "断点续跑记录：%s\n", journalPath)
	checkpointPath := journalPath + ".media.json"
	fmt.Fprintf(stderr, "阶段：正在处理 %d 个导出素材…\n", len(media))
	mediaOptions := mediaResolutionOptions{
		Plan:              plan,
		Media:             media,
		Target:            target,
		BundleDir:         outputDir,
		BundleRoot:        bundleRoot,
		CanvasJournalPath: journalPath,
		CheckpointPath:    checkpointPath,
		PollInterval:      dependencies.mediaPoll,
		WaitTimeout:       dependencies.mediaTimeout,
	}
	var resolved canvasplan.ResolvedMediaSet
	for {
		resolved, err = resolveImportMedia(ctx, mediaOptions, dependencies.media, stderr)
		if err == nil {
			break
		}
		if prompts != nil && errors.Is(err, errCanvasImportMediaStillProcessing) {
			fmt.Fprintln(stderr, "小云雀仍在处理已上传素材；持久化 ID 已保存，CLI 将继续只读查询，不会重复上传。")
			if waitErr := waitCanvasImportRetry(ctx, dependencies.mediaPoll); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		if prompts == nil || !isCanvasImportPippitAuthFailure(err) {
			return nil, err
		}
		fmt.Fprintln(stderr, "小云雀授权在素材处理期间失效；已保留安全断点，不会重复上传，重新授权后将继续。")
		if authErr := reauthenticateCanvasImportPippit(
			ctx, dependencies.pippitAuth, prompts.promptPippitAuth, authScope, stderr,
		); authErr != nil {
			return nil, authErr
		}
	}
	plan, err = canonicalizeImportPlanMedia(plan, target, journalPath, checkpointPath)
	if err != nil {
		return nil, fmt.Errorf("统一画布计划中的素材标识失败：%w", err)
	}
	fmt.Fprintln(stderr, "阶段：写入画布前再次确认小云雀授权…")
	var pippitPrompt importAuthPrompt
	if prompts != nil {
		pippitPrompt = prompts.promptPippitAuth
	}
	if err := ensureCanvasImportPippitAuthForScope(
		ctx, dependencies.pippitAuth, prompts != nil, pippitPrompt, authScope, stderr,
	); err != nil {
		return nil, err
	}
	result, handled, reconcileErr := reconcileExistingCanvasImport(
		ctx, journalPath, plan, resolved, opts, dependencies, stderr, prompts, authScope,
	)
	if handled {
		_ = removeOwnedBundle(outputDir, bundleRoot)
		return result, reconcileErr
	}
	fmt.Fprintln(stderr, "阶段：正在创建或续跑画布、写入节点与连线，并回读验证远端画布素材…")
	for {
		result, err = dependencies.executor.Execute(ctx, plan, resolved, canvasplan.ExecuteOptions{
			JournalPath: journalPath,
		})
		if err != nil {
			if prompts != nil && isCanvasImportPippitAuthFailure(err) && canvasImportStateCanRetryAfterAuth(result) {
				fmt.Fprintln(stderr, "小云雀授权在画布处理期间失效；断点已保存，重新授权后将从安全状态继续。")
				if authErr := reauthenticateCanvasImportPippit(
					ctx, dependencies.pippitAuth, prompts.promptPippitAuth, authScope, stderr,
				); authErr != nil {
					return result, authErr
				}
				continue
			}
			if prompts != nil && canvasImportStateCanContinueByQuery(result) {
				fmt.Fprintln(stderr, "远端画布写入状态暂时无法确认；断点已保存，CLI 将继续只读回查，不会重复提交写入。")
				if waitErr := waitCanvasImportRetry(ctx, dependencies.mediaPoll); waitErr != nil {
					return result, waitErr
				}
				continue
			}
			return result, fmt.Errorf("执行画布计划失败：%w", err)
		}
		if result != nil && result.State == canvasplan.StateCreatePending {
			if prompts != nil && isCanvasImportPippitAuthFailure(errors.New(result.Warning)) {
				fmt.Fprintln(stderr, "小云雀授权在等待漫剧画布创建期间失效；创建请求已受理，不会重复创建，重新授权后将继续等待。")
				if authErr := reauthenticateCanvasImportPippit(
					ctx, dependencies.pippitAuth, prompts.promptPippitAuth, authScope, stderr,
				); authErr != nil {
					return result, authErr
				}
				continue
			}
			fmt.Fprintln(stderr, "漫剧画布创建请求已受理，正在继续等待服务端完成；不会重复创建项目。")
			if waitErr := waitCanvasImportRetry(ctx, dependencies.mediaPoll); waitErr != nil {
				return result, waitErr
			}
			continue
		}
		break
	}
	return finishVerifiedCanvasImport(ctx, result, opts, dependencies, stderr)
}

func canvasImportStateCanContinueByQuery(result *canvasplan.ExecutionResult) bool {
	if result == nil {
		return false
	}
	return result.State == canvasplan.StateApplyAmbiguous ||
		result.State == canvasplan.StateVerificationFailed
}

func canvasImportStateCanRetryAfterAuth(result *canvasplan.ExecutionResult) bool {
	if result == nil {
		return false
	}
	switch result.State {
	case canvasplan.StateInitialized,
		canvasplan.StateCreatePending,
		canvasplan.StateRootReady,
		canvasplan.StateAllocationRequested,
		canvasplan.StateAllocated,
		canvasplan.StateMaterialized,
		canvasplan.StateApplyAmbiguous,
		canvasplan.StateVerificationFailed:
		return true
	default:
		return false
	}
}

func waitCanvasImportRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		delay = 2 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func exportLibTVCanvasWithRetry(
	ctx context.Context,
	sourceURL string,
	dependencies importDependencies,
	prompts *importPromptSession,
	stderr io.Writer,
) (string, string, *libTVExportResult, error) {
	for {
		bundleRoot, outputDir, err := newImportBundlePath(dependencies.userCacheDir)
		if err != nil {
			return "", "", nil, err
		}
		fmt.Fprintln(stderr, "阶段：正在导出 LibTV 画布及素材…")
		exported, exportErr := dependencies.exporter.Export(ctx, sourceURL, outputDir, stderr)
		if exportErr == nil {
			return bundleRoot, outputDir, exported, nil
		}
		_ = removeOwnedBundle(outputDir, bundleRoot)
		if ctx.Err() != nil {
			return "", "", nil, ctx.Err()
		}
		if prompts == nil {
			return "", "", nil, fmt.Errorf("导出 LibTV 画布失败：%w", exportErr)
		}
		fmt.Fprintf(stderr, "LibTV 导出未完成：%v\n", exportErr)
		choice, promptErr := prompts.askChoice(
			"LibTV 导出下一步：",
			[]importPromptChoice{
				{label: "重新检查授权并重试导出（默认）"},
				{label: "取消导入"},
			},
			1,
		)
		if promptErr != nil {
			return "", "", nil, promptErr
		}
		if choice == 2 {
			return "", "", nil, fmt.Errorf("已取消 LibTV 导出和画布导入")
		}
		if authErr := ensureLibTVImportAuth(ctx, dependencies.sourceAuth, prompts, stderr); authErr != nil {
			return "", "", nil, authErr
		}
	}
}

func reconcileExistingCanvasImport(
	ctx context.Context,
	journalPath string,
	plan canvasplan.Plan,
	resolved canvasplan.ResolvedMediaSet,
	opts importOptions,
	dependencies importDependencies,
	stderr io.Writer,
	prompts *importPromptSession,
	expectedCredentialScope string,
) (*canvasplan.ExecutionResult, bool, error) {
	info, err := os.Lstat(journalPath)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("检查画布导入断点记录以恢复任务失败：%w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, true, fmt.Errorf("画布导入断点记录必须是普通文件，不能是符号链接")
	}
	for {
		result, reconcileErr := dependencies.executor.Reconcile(ctx, journalPath, plan, resolved)
		if errors.Is(reconcileErr, canvasplan.ErrReconcileNotEligible) {
			return nil, false, nil
		}
		if result != nil && !journalOnlyReconcileState(result.State) {
			return nil, false, nil
		}
		if reconcileErr == nil {
			result, finishErr := finishVerifiedCanvasImport(ctx, result, opts, dependencies, stderr)
			return result, true, finishErr
		}
		if prompts != nil && isCanvasImportPippitAuthFailure(reconcileErr) {
			fmt.Fprintln(stderr, "小云雀授权在恢复画布断点期间失效；重新授权后将继续只读回查，不会重复提交写入。")
			if authErr := reauthenticateCanvasImportPippit(
				ctx, dependencies.pippitAuth, prompts.promptPippitAuth, expectedCredentialScope, stderr,
			); authErr != nil {
				return result, true, authErr
			}
			continue
		}
		if prompts != nil && canvasImportReconcileCanContinueByQuery(result, reconcileErr) {
			fmt.Fprintln(stderr, "远端画布状态暂时无法确认；CLI 将继续只读回查断点，不会重复提交写入。")
			if waitErr := waitCanvasImportRetry(ctx, dependencies.mediaPoll); waitErr != nil {
				return result, true, waitErr
			}
			continue
		}
		return result, true, fmt.Errorf("在不重复写入的前提下恢复画布导入失败：%w", reconcileErr)
	}
}

func canvasImportReconcileCanContinueByQuery(result *canvasplan.ExecutionResult, err error) bool {
	if result == nil || err == nil || !journalOnlyReconcileState(result.State) {
		return false
	}
	warning := strings.TrimSpace(result.Warning)
	return warning != "" && strings.TrimSpace(err.Error()) == warning
}

func journalOnlyReconcileState(state string) bool {
	switch state {
	case canvasplan.StateApplyAmbiguous, canvasplan.StateVerified, canvasplan.StateVerificationFailed:
		return true
	default:
		return false
	}
}

func finishVerifiedCanvasImport(
	ctx context.Context,
	result *canvasplan.ExecutionResult,
	opts importOptions,
	dependencies importDependencies,
	stderr io.Writer,
) (*canvasplan.ExecutionResult, error) {
	if !verifiedExecution(result) {
		return result, fmt.Errorf("画布计划已执行，但未通过回读验证")
	}
	if opts.Open {
		if err := validateTrustedCanvasURL(result); err != nil {
			return result, err
		}
		if err := dependencies.openURL(ctx, result.WebURL); err != nil {
			fmt.Fprintf(stderr, "画布已验证，但无法自动打开浏览器：%v\n", err)
		}
	}
	fmt.Fprintln(stderr, "阶段：画布导入已通过回读验证。")
	return result, nil
}

func normalizeLibTVURL(value string) (string, error) {
	raw := strings.TrimSpace(value)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return "", fmt.Errorf("canvas import 的 --url 必须是 HTTPS LibTV 画布链接")
	}
	host := strings.ToLower(parsed.Hostname())
	if (host != "www.liblib.tv" && host != "liblib.tv") || (parsed.Port() != "" && parsed.Port() != "443") {
		return "", fmt.Errorf("canvas import 的 --url 域名必须是 www.liblib.tv")
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != "/canvas" {
		return "", fmt.Errorf("canvas import 的 --url 必须指向带有 projectId 的 LibTV /canvas 项目")
	}
	if parsed.Fragment != "" {
		return "", fmt.Errorf("canvas import 的 --url 不能包含片段标识")
	}
	query := parsed.Query()
	projectIDs := query["projectId"]
	if len(projectIDs) != 1 || !libTVProjectIDPattern.MatchString(strings.TrimSpace(projectIDs[0])) {
		return "", fmt.Errorf("canvas import 的 --url 中 projectId 必须是 LibTV 项目 UUID")
	}
	canonical := &url.URL{Scheme: "https", Host: "www.liblib.tv", Path: "/canvas"}
	canonicalQuery := url.Values{}
	canonicalQuery.Set("projectId", strings.ToLower(strings.TrimSpace(projectIDs[0])))
	if spaceIDs, exists := query["spaceId"]; exists {
		if len(spaceIDs) != 1 || !libTVSpaceIDPattern.MatchString(strings.TrimSpace(spaceIDs[0])) {
			return "", fmt.Errorf("canvas import 的 --url 中 spaceId 必须是数字")
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
			return "", fmt.Errorf("解析画布导入断点记录路径失败：%w", err)
		}
		return filepath.Clean(absolute), nil
	}
	configDir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("解析画布导入配置目录失败：%w", err)
	}
	directory := filepath.Join(configDir, "pippit-cli", "canvas-import")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("创建画布导入断点记录目录失败：%w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return "", fmt.Errorf("设置画布导入断点记录目录权限失败：%w", err)
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

func legacyCanvasImportAuthScope(accessKey string) string {
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
		return fmt.Errorf("无法打开尚未验证的画布结果")
	}
	parsed, err := url.Parse(strings.TrimSpace(result.WebURL))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil ||
		strings.ToLower(parsed.Hostname()) != "xyq.jianying.com" ||
		(parsed.Port() != "" && parsed.Port() != "443") || parsed.Fragment != "" {
		return fmt.Errorf("无法打开不受信任的画布链接")
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != "/novel/detail/canvas" {
		return fmt.Errorf("无法打开非漫剧画布链接")
	}
	query := parsed.Query()
	if len(query["projectId"]) != 1 || query.Get("projectId") != result.ProjectID {
		return fmt.Errorf("画布链接中的项目 ID 与已验证结果不一致，无法打开")
	}
	if canvasIDs := query["canvasId"]; len(canvasIDs) > 1 ||
		(len(canvasIDs) == 1 && canvasIDs[0] != result.RootCanvasID) {
		return fmt.Errorf("画布链接中的画布 ID 与已验证结果不一致，无法打开")
	}
	for _, key := range []string{"overviewPippitAssetId", "overview_pippit_asset_id"} {
		if overviewIDs := query[key]; len(overviewIDs) > 1 ||
			(len(overviewIDs) == 1 && overviewIDs[0] != result.OverviewPippitAssetID) {
			return fmt.Errorf("画布链接中的总览 ID 与已验证结果不一致，无法打开")
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
	command.Env = sanitizedCanvasOpenEnv(os.Environ())
	if err := command.Run(); err != nil {
		return fmt.Errorf("打开画布链接失败：%w", err)
	}
	return nil
}

func sanitizedCanvasOpenEnv(environ []string) []string {
	result := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "XYQ_ACCESS_KEY", "PIPPIT_ACCESS_KEY", "PIPPIT_AK":
			continue
		default:
			result = append(result, entry)
		}
	}
	return result
}
