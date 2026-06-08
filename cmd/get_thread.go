package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

func newGetThreadCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts common.GetThreadOptions

	cmd := &cobra.Command{
		Use:   "get-thread",
		Short: "Get a thread detail",
		Args:  cobra.NoArgs,
		RunE: withErrorLog("get-thread", func() map[string]string {
			return map[string]string{
				"thread_id": opts.ThreadID,
				"run_id":    opts.RunID,
			}
		}, func(cmd *cobra.Command, _ []string) error {
			opts.ThreadID = strings.TrimSpace(opts.ThreadID)
			if opts.ThreadID == "" {
				return fmt.Errorf("--thread-id is required")
			}
			opts.RunID = strings.TrimSpace(opts.RunID)
			opts.Version = common.GetThreadVersionV2

			result, err := common.GetThread(cmd.Context(), &opts, runner)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(stdout, result.ReadableText)
			return err
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "thread ID to fetch")
	cmd.Flags().StringVar(&opts.RunID, "run-id", "", "run ID to fetch")
	return cmd
}

func withErrorLog(command string, fields func() map[string]string, run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		if err != nil {
			logFields := map[string]string(nil)
			if fields != nil {
				logFields = fields()
			}
			_ = common.AppendDailyErrorLog(command, err, logFields)
		}
		return err
	}
}
