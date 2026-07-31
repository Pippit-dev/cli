package video_tool

import (
	"io"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internaltool "github.com/Pippit-dev/pippit-cli/internal/video_tool"
	"github.com/spf13/cobra"
)

// NewSuperResolutionCommand builds the video-super-resolution command.
func NewSuperResolutionCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	opts := &internaltool.SuperResolutionOptions{}
	cmd := &cobra.Command{
		Use:   "video-super-resolution",
		Short: "Improve the resolution of one video",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := internaltool.RunSuperResolution(cmd.Context(), opts, runner)
			if err != nil {
				_ = common.AppendDailyErrorLog("video-super-resolution", err, map[string]string{
					"video_path":        opts.VideoPath,
					"tool_version":      opts.ToolVersion,
					"output_resolution": opts.OutputResolution,
				})
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&opts.VideoPath, "video", "", "local input video path")
	flags.StringVar(&opts.OutputResolution, "output-resolution", "", "output resolution passed to the service: 720p, 1080p, 2k, or 4k")
	flags.StringVar(&opts.ToolVersion, "tool-version", "", "optional tool version passed to the service: standard, professional_v1, or professional_v2")
	return cmd
}

// NewEraseSubtitleCommand builds the erase-video-subtitle command.
func NewEraseSubtitleCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	opts := &internaltool.EraseSubtitleOptions{}
	cmd := &cobra.Command{
		Use:   "erase-video-subtitle",
		Short: "Erase subtitles from one video",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := internaltool.RunEraseSubtitle(cmd.Context(), opts, runner)
			if err != nil {
				_ = common.AppendDailyErrorLog("erase-video-subtitle", err, map[string]string{
					"video_path": opts.VideoPath,
				})
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&opts.VideoPath, "video", "", "local input video path")
	return cmd
}
