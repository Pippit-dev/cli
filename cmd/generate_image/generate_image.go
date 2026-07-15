package generate_image

import (
	"io"
	"strconv"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internalgen "github.com/Pippit-dev/pippit-cli/internal/generate_image"
	"github.com/spf13/cobra"
)

// NewCommand builds the generate-image command.
func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	opts := &internalgen.Options{}
	var generateImageCount int

	cmd := &cobra.Command{
		Use:   "generate-image",
		Short: "Generate an image with the nest agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("generate-image-count") {
				opts.GenerateImageCount = &generateImageCount
			} else {
				opts.GenerateImageCount = nil
			}

			result, err := internalgen.Run(cmd.Context(), opts, runner)
			if err != nil {
				_ = common.AppendDailyErrorLog("generate-image", err, map[string]string{
					"prompt":               strings.TrimSpace(opts.Prompt),
					"image":                strings.Join(opts.ImagePaths, ","),
					"model":                strings.TrimSpace(opts.Model),
					"ratio":                strings.TrimSpace(opts.Ratio),
					"resolution":           strings.ToUpper(strings.TrimSpace(opts.Resolution)),
					"generate_image_count": optionalIntString(opts.GenerateImageCount),
				})
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&opts.Prompt, "prompt", "", "image generation prompt")
	flags.StringArrayVar(&opts.ImagePaths, "image", nil, "local reference image path; repeat for multiple images")
	flags.StringVar(&opts.Model, "model", "", "image model; supported: seedream_5.0_pro, seedream_5.0, seedream_4.3, nova2, seedream_4.5, seedream_4.1, seedream_4")
	flags.StringVar(&opts.Ratio, "ratio", "", "image ratio; "+internalgen.SupportedRatioUsage())
	flags.StringVar(&opts.Resolution, "resolution", "", "image resolution; only seedream_5.0_pro supports these options: 1K, 2K, 4K")
	flags.IntVar(&generateImageCount, "generate-image-count", 0, "generated image count")
	return cmd
}

func optionalIntString(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}
