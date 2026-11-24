package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/commandnames"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

func newGetRunCreditsCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts common.GetRunCreditsOptions
	cmd := &cobra.Command{
		Use:   commandnames.GetRunCredits,
		Short: "Get credit consumption for a Run",
		Args:  cobra.NoArgs,
		RunE: withErrorLog(commandnames.GetRunCredits, nil, func(cmd *cobra.Command, _ []string) error {
			opts.RunID = strings.TrimSpace(opts.RunID)
			opts.ThreadID = strings.TrimSpace(opts.ThreadID)
			if opts.RunID == "" {
				return fmt.Errorf("--run-id is required")
			}
			result, err := common.GetRunCredits(cmd.Context(), opts, runner)
			if err != nil {
				return err
			}
			return common.WriteJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.RunID, "run-id", "", "Run ID returned by submission")
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "optional expected thread ID")
	return cmd
}
