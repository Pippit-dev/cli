package generate_video

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internalgen "github.com/Pippit-dev/pippit-cli/internal/generate_video"
	"github.com/bytedance/sonic"
	"github.com/spf13/cobra"
)

// NewCommand builds the generate_video command.
func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	cmd := &cobra.Command{
		Use:                "generate_video",
		Short:              "Generate a video with the video part agent",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if wantsHelp(args) {
				return cmd.Help()
			}

			opts, err := parseArgs(args)
			if err != nil {
				_ = common.AppendDailyErrorLog("generate_video", err, nil)
				return err
			}

			result, err := internalgen.Run(cmd.Context(), opts, runner)
			if err != nil {
				_ = common.AppendDailyErrorLog("generate_video", err, map[string]string{
					"image_count": strconv.Itoa(len(opts.ImagePaths)),
					"video_count": strconv.Itoa(len(opts.VideoPaths)),
				})
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	addHelpFlags(cmd)
	return cmd
}

func addHelpFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.String("prompt", "", "video generation prompt")
	flags.StringArray("images", nil, "local reference image path; accepts multiple values")
	flags.StringArray("videos", nil, "local reference video path; accepts multiple values")
	flags.Int("duration", 0, "video duration in seconds")
	flags.String("ratio", "", "video ratio, such as 9:16")
	flags.String("model", "", "video model")
	flags.String("resolution", "", "video resolution")
	flags.Int("generate-type", 0, "generation type")
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func parseArgs(args []string) (*internalgen.Options, error) {
	opts := &internalgen.Options{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return nil, fmt.Errorf("unexpected argument %q", arg)
		}

		name, value, hasInlineValue := strings.Cut(arg, "=")
		switch name {
		case "--prompt":
			v, next, err := readSingleValue(args, i, value, hasInlineValue, name)
			if err != nil {
				return nil, err
			}
			opts.Prompt = v
			i = next
		case "--duration":
			v, next, err := readIntValue(args, i, value, hasInlineValue, name)
			if err != nil {
				return nil, err
			}
			opts.DurationSec = &v
			i = next
		case "--ratio":
			v, next, err := readSingleValue(args, i, value, hasInlineValue, name)
			if err != nil {
				return nil, err
			}
			opts.Ratio = v
			i = next
		case "--model":
			v, next, err := readSingleValue(args, i, value, hasInlineValue, name)
			if err != nil {
				return nil, err
			}
			opts.Model = v
			i = next
		case "--resolution":
			v, next, err := readSingleValue(args, i, value, hasInlineValue, name)
			if err != nil {
				return nil, err
			}
			opts.Resolution = v
			i = next
		case "--generate-type":
			v, next, err := readIntValue(args, i, value, hasInlineValue, name)
			if err != nil {
				return nil, err
			}
			opts.GenerateType = &v
			i = next
		case "--images":
			values, next, err := readListValues(args, i, value, hasInlineValue, name)
			if err != nil {
				return nil, err
			}
			opts.ImagePaths = append(opts.ImagePaths, values...)
			i = next
		case "--videos":
			values, next, err := readListValues(args, i, value, hasInlineValue, name)
			if err != nil {
				return nil, err
			}
			opts.VideoPaths = append(opts.VideoPaths, values...)
			i = next
		default:
			return nil, fmt.Errorf("unknown flag: %s", name)
		}
	}
	return opts, nil
}

func readSingleValue(args []string, index int, inline string, hasInline bool, name string) (string, int, error) {
	if hasInline {
		return inline, index, nil
	}
	if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
		return "", index, fmt.Errorf("%s requires a value", name)
	}
	return args[index+1], index + 1, nil
}

func readIntValue(args []string, index int, inline string, hasInline bool, name string) (int, int, error) {
	raw, next, err := readSingleValue(args, index, inline, hasInline, name)
	if err != nil {
		return 0, index, err
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, index, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return value, next, nil
}

func readListValues(args []string, index int, inline string, hasInline bool, name string) ([]string, int, error) {
	if hasInline {
		if inline == "" {
			return nil, index, fmt.Errorf("%s requires at least one value", name)
		}
		return []string{inline}, index, nil
	}

	values := make([]string, 0)
	next := index
	for next+1 < len(args) && !strings.HasPrefix(args[next+1], "--") {
		next++
		values = append(values, args[next])
	}
	if len(values) == 0 {
		return nil, index, fmt.Errorf("%s requires at least one value", name)
	}
	return values, next, nil
}

func writeJSON(w io.Writer, v any) error {
	data, err := sonic.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
