package cmd

import (
	"io"
	"os"

	// authcmd "github.com/Pippit-dev/pippit-cli/cmd/auth"
	"github.com/Pippit-dev/pippit-cli/cmd/short_drama"
	updatecmd "github.com/Pippit-dev/pippit-cli/cmd/update"
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
	client := common.NewHTTPClient(cfg.BaseURL, cfg.HTTPTimeout, common.NewAccessKeyAuthorizer(cfg.AccessKey))
	runner := common.NewRunner(cfg, client)
	return newRootCommand(stdout, stderr, runner)
}

func newRootCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	root := &cobra.Command{
		Use:           "pippit-tool-cli",
		Short:         "Pippit CLI",
		Long:          "Pippit CLI submits short-drama workflows, downloads generated assets, and updates the installed CLI package.",
		Version:       version.Current(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	// root.AddCommand(authcmd.NewCommand(stdout, stderr, runner)) // temporarily disabled; auth is via access key injection
	root.AddCommand(newDownloadResultCommand(stdout, stderr, runner))
	root.AddCommand(newGetThreadCommand(stdout, stderr, runner))
	root.AddCommand(newListThreadFileCommand(stdout, stderr, runner))
	root.AddCommand(newUploadFileCommand(stdout, stderr, runner))
	root.AddCommand(short_drama.NewCommand(stdout, stderr, runner))
	root.AddCommand(updatecmd.NewCommand(stdout, stderr))
	return root
}
