package cmd

import (
	"io"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/commandnames"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

func newIPCommand(stdout, stderr io.Writer, runner *common.Runner, policy adapter.HTTPAdapter) *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandnames.IP,
		Short: "Query the outbound IP observed by the server",
		Args:  cobra.NoArgs,
		RunE: withErrorLog(commandnames.IP, nil, func(cmd *cobra.Command, _ []string) error {
			result, err := common.GetClientIP(cmd.Context(), runner, policy)
			if err != nil {
				return err
			}
			return common.WriteJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return cmd
}
