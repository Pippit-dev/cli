package generate_video

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internalgen "github.com/Pippit-dev/pippit-cli/internal/generate_video"
	"github.com/spf13/cobra"
)

// Audio queries opt in explicitly because an error without data cannot identify
// whether the requested Run belongs to the legacy image/video or audio workflow.
func runAudioQueryResult(cmd *cobra.Command, stdout io.Writer, opts *internalgen.QueryResultOptions, runner *common.Runner) error {
	result, err := internalgen.QueryAudioResult(cmd.Context(), opts, runner)
	if err != nil {
		_ = common.AppendDailyErrorLog("query-result", err, map[string]string{
			"thread_id": strings.TrimSpace(opts.ThreadID), "run_id": strings.TrimSpace(opts.RunID),
			"download_dir": strings.TrimSpace(opts.DownloadDir),
		})
		result = &internalgen.QueryAudioResultResult{
			ThreadID: strings.TrimSpace(opts.ThreadID), RunID: strings.TrimSpace(opts.RunID),
			ErrorMessage: err.Error(), Videos: []internalgen.QueryResultVideo{},
			Images: []internalgen.QueryResultImage{}, Audios: []internalgen.QueryResultAudio{},
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
