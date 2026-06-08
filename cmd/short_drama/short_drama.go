package short_drama

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/short_drama"
	"github.com/bytedance/sonic"
	"github.com/spf13/cobra"
)

// NewCommand builds the short_drama scene command tree.
func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "short-drama",
		Short: "Short drama generation workflows",
	}

	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.AddCommand(newShortDramaSubmitRunCommand(stdout, stderr, runner))
	return cmd
}

func newShortDramaSubmitRunCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts short_drama.SubmitRunOptions

	cmd := &cobra.Command{
		Use:   "+submit-run",
		Short: "Submit a Run task for the short drama scene",
		Args:  cobra.NoArgs,
		RunE: withErrorLog("short-drama +submit-run", func() map[string]string {
			return map[string]string{
				"thread_id":   opts.ThreadID,
				"asset_count": strconv.Itoa(len(opts.AssetIDs)),
			}
		}, func(cmd *cobra.Command, _ []string) error {
			opts.Message = strings.TrimSpace(opts.Message)
			opts.ThreadID = strings.TrimSpace(opts.ThreadID)

			if opts.Message == "" {
				return fmt.Errorf("--message is required")
			}

			result, err := short_drama.SubmitRun(cmd.Context(), &opts, runner)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.Message, "message", "", "message to send to the short drama agent")
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "existing thread ID; omit to create a new thread")
	cmd.Flags().StringArrayVar(&opts.AssetIDs, "asset-ids", nil, "asset ID to attach; repeat for multiple assets")
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

func writeJSON(w io.Writer, v any) error {
	data, err := sonic.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
