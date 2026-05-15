package cmd

import (
	"io"
	"os"

	novelcmd "github.com/Pippit-dev/pippit-cli/cmd/novel"
	"github.com/Pippit-dev/pippit-cli/internal"
	"github.com/spf13/cobra"
)

// Execute runs the pippit-cli command tree.
func Execute() error {
	return NewRootCommand(os.Stdout, os.Stderr).Execute()
}

func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	apiClient := internal.New(os.Getenv("PIPPIT_CLI_BASE_URL"))

	root := &cobra.Command{
		Use:           "pippit-cli",
		Short:         "Pippit CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(novelcmd.NewCommand(stdout, stderr, apiClient))
	return root
}
