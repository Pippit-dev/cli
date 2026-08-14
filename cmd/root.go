package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	authcmd "github.com/Pippit-dev/pippit-cli/cmd/auth"
	"github.com/Pippit-dev/pippit-cli/cmd/generate_image"
	"github.com/Pippit-dev/pippit-cli/cmd/generate_video"
	"github.com/Pippit-dev/pippit-cli/cmd/short_drama"
	updatecmd "github.com/Pippit-dev/pippit-cli/cmd/update"
	"github.com/Pippit-dev/pippit-cli/cmd/video_tool"
	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/version"
	"github.com/spf13/cobra"
)

// Execute runs the pippit-tool-cli command tree.
func Execute() error {
	return NewRootCommand(os.Stdout, os.Stderr).Execute()
}

func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	cfg := config.Load()
	runner := newRootRunner(cfg)
	return newRootCommand(stdout, stderr, runner)
}

func newRootRunner(cfg *config.Config) *common.Runner {
	runner := common.NewRunner(cfg, nil)
	runner.Auth = internal_auth.NewManager(cfg)
	runner.Client = common.NewHTTPClient(
		cfg.BaseURL,
		cfg.HTTPTimeout,
		newRunnerAuthorizer(runner),
	)
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
	root := &cobra.Command{
		Use:           "pippit-tool-cli",
		Short:         "Pippit CLI",
		Long:          "Pippit CLI generates and processes videos and images, submits short-drama workflows, downloads generated assets, and updates the installed CLI package.",
		Version:       version.Current(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(authcmd.NewLoginCommand(stdout, stderr, runner))
	root.AddCommand(authcmd.NewStatusCommand(stdout, stderr, runner))
	root.AddCommand(authcmd.NewLogoutCommand(stdout, stderr, runner))
	root.AddCommand(newDownloadResultCommand(stdout, stderr, runner))
	root.AddCommand(newGetThreadCommand(stdout, stderr, runner))
	root.AddCommand(newListThreadFileCommand(stdout, stderr, runner))
	root.AddCommand(generate_image.NewCommand(stdout, stderr, runner))
	root.AddCommand(generate_video.NewCommand(stdout, stderr, runner))
	root.AddCommand(generate_video.NewQueryResultCommand(stdout, stderr, runner))
	root.AddCommand(video_tool.NewSuperResolutionCommand(stdout, stderr, runner))
	root.AddCommand(video_tool.NewEraseSubtitleCommand(stdout, stderr, runner))
	root.AddCommand(short_drama.NewCommand(stdout, stderr, runner))
	root.AddCommand(updatecmd.NewCommand(stdout, stderr))
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
