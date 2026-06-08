package generate_video

import (
	"fmt"
	"io"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internalgen "github.com/Pippit-dev/pippit-cli/internal/generate_video"
	"github.com/spf13/cobra"
)

// NewQueryResultCommand builds the query_result command.
func NewQueryResultCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	opts := &internalgen.QueryResultOptions{}

	cmd := &cobra.Command{
		Use:   "query_result",
		Short: "Query a generate_video run result and download completed videos",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := internalgen.QueryResult(cmd.Context(), opts, runner)
			if err != nil {
				_ = common.AppendDailyErrorLog("query_result", err, map[string]string{
					"thread_id":    strings.TrimSpace(opts.ThreadID),
					"run_id":       strings.TrimSpace(opts.RunID),
					"download_dir": strings.TrimSpace(opts.DownloadDir),
				})
				return err
			}
			if !result.Completed {
				_, err = fmt.Fprintf(stdout, "Run 尚未完成，当前状态：%d\n请稍后重试 query_result。\n", result.State)
				return err
			}
			_, err = fmt.Fprintln(stdout, "Run 已完成，产物已下载：")
			if err != nil {
				return err
			}
			for _, path := range result.OutputPaths {
				if _, err := fmt.Fprintln(stdout, path); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "thread ID to fetch")
	cmd.Flags().StringVar(&opts.RunID, "run-id", "", "run ID to fetch")
	cmd.Flags().StringVar(&opts.DownloadDir, "download-dir", "", "directory to download completed videos into")
	return cmd
}
