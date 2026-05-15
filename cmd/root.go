package cmd

import (
	"io"
	"os"

	novelcmd "github.com/Pippit-dev/pippit-cli/cmd/novel"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/spf13/cobra"
)

// Execute runs the pippit-cli command tree.
func Execute() error {
	return NewRootCommand(os.Stdout, os.Stderr).Execute()
}

func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	cfg := config.Load()
	runner := common.NewRunner(cfg)

	root := &cobra.Command{
		Use:           "pippit-cli",
		Short:         "Pippit CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(novelcmd.NewCommand(stdout, stderr, runner))
	return root
}
