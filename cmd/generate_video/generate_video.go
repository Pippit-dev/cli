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

	cmd := &cobra.Command{
		Use:   "generate-video",
		Short: "Generate a video with the video part agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("duration") {
				opts.DurationSec = &durationSec
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
	flags.StringVar(&opts.Prompt, "prompt", "", "video generation prompt")
	flags.StringArrayVar(&opts.ImagePaths, "image", nil, "local reference image path; repeat for multiple images, up to 9")
	flags.StringArrayVar(&opts.VideoPaths, "video", nil, "local reference video path; repeat for multiple videos, up to 3")
	flags.StringArrayVar(&opts.AudioPaths, "audio", nil, "local reference audio path; repeat for multiple audios, up to 3")
	flags.IntVar(&durationSec, "duration", 0, "video duration in seconds")
	flags.StringVar(&opts.Ratio, "ratio", "", "video ratio, such as 9:16, 16:9, 3:4, 4:3")
	flags.StringVar(&opts.Model, "model", "", "video model; normal users: Seedance_2.0_mini_lite; VIP-only: seedance2.0_vision, seedance2.0_fast_vision, Seedance_2.0_mini")
	flags.StringVar(&opts.Resolution, "resolution", "", "video resolution, such as 720p, 1080p")
	return cmd
}
