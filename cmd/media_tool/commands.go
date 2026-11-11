package media_tool

import (
	"github.com/Pippit-dev/pippit-cli/internal/commandnames"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	tool "github.com/Pippit-dev/pippit-cli/internal/media_tool"
	"github.com/spf13/cobra"
	"io"
)

func NewImageSuperResolutionCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	cmd, opts := newCommand(commandnames.ImageSuperResolution, "Improve one uploaded image's resolution", stdout, stderr, runner)
	cmd.Flags().StringVar(&opts.Quality, "quality", "", "optional quality: LQ, MQ, HQ; service default MQ")
	return cmd
}
func NewVideoMattingCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	cmd, opts := newCommand(commandnames.VideoMatting, "Remove a video's background and output a black or green background MP4", stdout, stderr, runner)
	cmd.Flags().StringVar(&opts.BackgroundColor, "background-color", "", "optional MP4 background: black or green; service default black (no alpha channel)")
	return cmd
}
func newCommand(name, short string, stdout, stderr io.Writer, runner *common.Runner) (*cobra.Command, *tool.Options) {
	opts := &tool.Options{Tool: name}
	cmd := &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs, Long: short + ".\nUse an asset_id from upload-file in the same space. Recovery IDs are printed to stderr before submission. Use query-result to query and download; never resubmit after an uncertain response. This command does not update Canvas nodes.", RunE: func(cmd *cobra.Command, _ []string) error {
		result, err := tool.Submit(cmd.Context(), *opts, runner, func(r *tool.Result) error { return common.WriteJSON(stderr, r) })
		if result != nil {
			if writeErr := common.WriteJSON(stdout, result); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			fields := map[string]string{"asset_id": opts.AssetID}
			if result != nil {
				fields["thread_id"] = result.ThreadID
				fields["run_id"] = result.RunID
				fields["team_id"] = result.TeamID
			}
			_ = common.AppendDailyErrorLog(name, err, fields)
		}
		return err
	}}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.AssetID, "asset-id", "", "uploaded Pippit asset ID in the selected personal/team space")
	cmd.Flags().StringVar(&opts.Source, "source", "", "optional stable host platform identifier for statistics")
	return cmd, opts
}
