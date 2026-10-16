package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	authcmd "github.com/Pippit-dev/pippit-cli/cmd/auth"
	canvascmd "github.com/Pippit-dev/pippit-cli/cmd/canvas"
	"github.com/Pippit-dev/pippit-cli/cmd/generate_image"
	"github.com/Pippit-dev/pippit-cli/cmd/generate_video"
	"github.com/Pippit-dev/pippit-cli/cmd/short_drama"
	updatecmd "github.com/Pippit-dev/pippit-cli/cmd/update"
	"github.com/Pippit-dev/pippit-cli/cmd/video_tool"
	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/adaptercli"
	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/version"
	"github.com/spf13/cobra"
)

// Execute runs the pippit-tool-cli command tree.
func Execute() error { return ExecuteWithAdapter(adapter.Default{}) }

func ExecuteWithAdapter(policy adapter.Adapter) error {
	cfg, err := config.LoadFile(config.FileName)
	if err != nil {
		return err
	}
	policy = adapter.OrDefault(policy)
	runner := newRootRunnerWithAdapter(cfg, policy)
	return newRootCommandWithAdapter(os.Stdout, os.Stderr, runner, policy).Execute()
}

func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	return NewRootCommandWithAdapter(stdout, stderr, adapter.Default{})
}

func NewRootCommandWithAdapter(stdout, stderr io.Writer, policy adapter.Adapter) *cobra.Command {
	policy = adapter.OrDefault(policy)
	cfg := config.Load()
	runner := newRootRunnerWithAdapter(cfg, policy)
	return newRootCommandWithAdapter(stdout, stderr, runner, policy)
}

func newRootRunner(cfg *config.Config) *common.Runner {
	return newRootRunnerWithAdapter(cfg, adapter.Default{})
}

func newRootRunnerWithAdapter(cfg *config.Config, policy adapter.Adapter) *common.Runner {
	runner := common.NewRunner(cfg, nil)
	var authorizer common.RequestAuthorizer
	options := policy.CLIOptions()
	if options.LocalCredentialsEnabled {
		runner.Auth = internal_auth.NewManager(cfg, internal_auth.WithCLIOptions(options))
		authorizer = newRunnerAuthorizer(runner)
	}
	runner.Client = common.NewHTTPClientWithAdapter(cfg.BaseURL, cfg.HTTPTimeout, authorizer, adapter.WithPPE(policy, cfg.PPEEnv))
	return runner
}

func newRunnerAuthorizer(runner *common.Runner) common.RequestAuthorizer {
	return common.NewAccessKeyContextProviderAuthorizer(func(ctx context.Context) (string, error) {
		if runner.Auth == nil {
			return "", nil
		}
		return runner.Auth.ResolveAccessKey(ctx)
	})
}

func newRootCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	return newRootCommandWithAdapter(stdout, stderr, runner, adapter.Default{})
}

func newRootCommandWithAdapter(stdout, stderr io.Writer, runner *common.Runner, policy adapter.Adapter) *cobra.Command {
	root := &cobra.Command{
		Use:   "pippit-tool-cli",
		Short: "Pippit CLI",
		Long: "Pippit CLI generates and processes videos and images, queries credit balances, submits short-drama workflows, downloads generated assets, and updates the installed CLI package.\n\n" +
			canvascmd.CommandDiscoveryHelp + "\n\nInstallation help (npm launcher): pippit-tool-cli install --help",
		Version:       version.Current(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Only commands already exposing --source receive attribution. Missing or blank
	// flags allow fallback, and no source is persisted between CLI processes.
	// 子命令执行 pre hook
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		flag := cmd.Flags().Lookup("source")
		if flag != nil && (!flag.Changed || strings.TrimSpace(flag.Value.String()) == "") {
			if source := common.DetectHostSource(); source != "" {
				if err := cmd.Flags().Set("source", source); err != nil {
					return err
				}
			}
		}
		return prepareCommandSpace(cmd, runner, stderr)
	}
	// 定义根命令和子命令都能使用的字符串参数
	root.PersistentFlags().String("team-id", "", "本次业务操作的空间：0 为个人空间，正整数为团队空间；不改变默认空间")
	root.PersistentFlags().String("space-context", "", "固定跨进程操作的账号、API origin 和空间")
	_ = root.PersistentFlags().MarkHidden("space-context")
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(authcmd.NewLoginCommand(stdout, stderr, runner))
	root.AddCommand(authcmd.NewStatusCommand(stdout, stderr, runner))
	root.AddCommand(authcmd.NewLogoutCommand(stdout, stderr, runner))
	root.AddCommand(newSpaceCommand(stdout, runner))
	root.AddCommand(canvascmd.NewCommand(stdout, stderr, runner))
	root.AddCommand(newDownloadResultCommand(stdout, stderr, runner))
	root.AddCommand(newGetCreditBalanceCommand(stdout, stderr, runner))
	root.AddCommand(newModelCommand(stdout, stderr, runner))
	root.AddCommand(newMarketingCommand(stdout, stderr, runner, policy))
	root.AddCommand(newGetThreadCommand(stdout, stderr, runner))
	// 暂停对外提供通用综合 Agent 提交入口，保留实现供后续恢复。
	// root.AddCommand(newSubmitRunCommand(stdout, stderr, runner))
	root.AddCommand(newUploadFileCommand(stdout, stderr, runner))
	root.AddCommand(newListThreadFileCommand(stdout, stderr, runner))
	root.AddCommand(generate_image.NewCommand(stdout, stderr, runner))
	root.AddCommand(generate_video.NewCommand(stdout, stderr, runner))
	root.AddCommand(generate_video.NewQueryResultCommand(stdout, stderr, runner))
	root.AddCommand(video_tool.NewSuperResolutionCommand(stdout, stderr, runner))
	root.AddCommand(video_tool.NewEraseSubtitleCommand(stdout, stderr, runner))
	root.AddCommand(short_drama.NewCommand(stdout, stderr, runner))
	root.AddCommand(updatecmd.NewCommandWithAdapter(stdout, stderr, policy))
	adaptercli.Configure(root, policy.CLIOptions())
	localizeFlagErrors(root)
	return root
}

func localizeFlagErrors(cmd *cobra.Command) {
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return localizeFlagError(err)
	})
	for _, child := range cmd.Commands() {
		localizeFlagErrors(child)
	}
}

func localizeFlagError(err error) error {
	msg := err.Error()
	if flag, ok := strings.CutPrefix(msg, "unknown flag: "); ok {
		return fmt.Errorf("未知参数: %s", flag)
	}
	if flag, ok := strings.CutPrefix(msg, "flag needs an argument: "); ok {
		return fmt.Errorf("参数 %s 缺少取值", flag)
	}
	return fmt.Errorf("参数解析失败: %s", msg)
}
