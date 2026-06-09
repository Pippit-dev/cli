package generate_video

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internalgen "github.com/Pippit-dev/pippit-cli/internal/generate_video"
	"github.com/spf13/cobra"
)

// NewQueryResultCommand builds the query-result command.
func NewQueryResultCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	opts := &internalgen.QueryResultOptions{}

	cmd := &cobra.Command{
		Use:   "query-result",
		Short: "Query a generate-video run result and download completed videos",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := internalgen.QueryResult(cmd.Context(), opts, runner)
			if err != nil {
				_ = common.AppendDailyErrorLog("query-result", err, map[string]string{
					"thread_id":    strings.TrimSpace(opts.ThreadID),
					"run_id":       strings.TrimSpace(opts.RunID),
					"download_dir": strings.TrimSpace(opts.DownloadDir),
				})
				return err
			}
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "thread_id from generate-video output")
	cmd.Flags().StringVar(&opts.RunID, "run-id", "", "run_id from generate-video output")
	cmd.Flags().StringVar(&opts.DownloadDir, "download-dir", "", "directory to download completed videos into")
	return cmd
}
