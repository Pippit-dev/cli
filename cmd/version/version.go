package versioncmd

import (
	"fmt"
	"io"

	"github.com/Pippit-dev/pippit-cli/internal/version"
	"github.com/spf13/cobra"
)

func NewCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print pippit-cli version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(stdout, version.Current())
			return err
		},
	}
}
