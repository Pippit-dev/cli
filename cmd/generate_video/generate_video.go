package generate_video

import (
	"io"
	"strconv"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internalgen "github.com/Pippit-dev/pippit-cli/internal/generate_video"
	"github.com/spf13/cobra"
)

// NewCommand builds the generate_video command.
func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	opts := &internalgen.Options{}
	var durationSec int
	var generateType int

	cmd := &cobra.Command{
		Use:   "generate_video",
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
				_ = common.AppendDailyErrorLog("generate_video", err, map[string]string{
					"image_count": strconv.Itoa(len(opts.ImagePaths)),
					"video_count": strconv.Itoa(len(opts.VideoPaths)),
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
	flags.StringArrayVar(&opts.ImagePaths, "image", nil, "local reference image path; repeat for multiple images")
	flags.StringArrayVar(&opts.VideoPaths, "video", nil, "local reference video path; repeat for multiple videos")
	flags.IntVar(&durationSec, "duration", 0, "video duration in seconds")
	flags.StringVar(&opts.Ratio, "ratio", "", "video ratio, such as 9:16")
	flags.StringVar(&opts.Model, "model", "", "video model")
	flags.StringVar(&opts.Resolution, "resolution", "", "video resolution")
	flags.IntVar(&generateType, "generate-type", 0, "generation type")
	return cmd
}
