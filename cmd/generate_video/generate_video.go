package generate_video

import (
	"io"
	"strconv"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internalgen "github.com/Pippit-dev/pippit-cli/internal/generate_video"
	"github.com/spf13/cobra"
)

// NewCommand builds the generate-video command.
func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	opts := &internalgen.Options{}
	var durationSec int
	var generateType int64

	cmd := &cobra.Command{
		Use:   "generate-video",
		Short: "Generate a video with the video part agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("duration") {
				opts.DurationSec = &durationSec
			}
			if cmd.Flags().Changed("generate-type") {
				opts.GenerateType = &generateType
			}

			result, err := internalgen.Run(cmd.Context(), opts, runner)
			if err != nil {
				_ = common.AppendDailyErrorLog("generate-video", err, map[string]string{
					"image_count": strconv.Itoa(len(opts.ImagePaths)),
					"video_count": strconv.Itoa(len(opts.VideoPaths)),
					"audio_count": strconv.Itoa(len(opts.AudioPaths)),
				})
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&opts.Source, "source", "", "optional host agent/platform identifier for statistics only; filled silently by the host agent (e.g. doubao_office, workbuddy, codex)")
	flags.StringVar(&opts.Prompt, "prompt", "", "video generation prompt")
	flags.StringArrayVar(&opts.ImagePaths, "image", nil, "local reference image path; repeat for multiple images")
	flags.StringArrayVar(&opts.VideoPaths, "video", nil, "local reference video path; repeat for multiple videos")
	flags.StringArrayVar(&opts.AudioPaths, "audio", nil, "local reference audio path; repeat for multiple audios")
	flags.IntVar(&durationSec, "duration", 0, "video duration in seconds")
	flags.StringVar(&opts.Ratio, "ratio", "", "video ratio, such as 9:16, 16:9, 3:4, 4:3")
	flags.StringVar(&opts.Model, "model", "", "video model key; use 'model list' to discover available models")
	flags.StringVar(&opts.Resolution, "resolution", "", "video resolution; optional for Seedance_2.0_mini and Seedance_2.0_mini_lite (server defaults to 720p); use 'model describe <key>' for current configuration")
	flags.Int64Var(&generateType, "generate-type", 0, "generation type passed to the service; set 1 for first-and-last-frame generation and provide --image values in first-frame, last-frame order; MiniMax and Wan also accept a single first-frame image in this mode")
	return cmd
}
