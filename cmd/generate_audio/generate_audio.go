package generate_audio

import (
	"io"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internalgen "github.com/Pippit-dev/pippit-cli/internal/generate_audio"
	"github.com/spf13/cobra"
)

func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	opts := &internalgen.Options{}
	var sampleRate int32
	var speechRate, loudnessRate, pitchRate float64
	var format string
	var enableTimestamp bool
	cmd := &cobra.Command{
		Use:   "generate-audio",
		Short: "Generate audio with Seed Audio 1.0",
		Long: "Generate audio with Seed Audio 1.0. Only seedaudio_1.0 is supported; other audio models and modes are not integrated. This command does not cover all audio features in the web app.\n\n" +
			"Reference image upload and task submission are connected, but successful audio generation from an image has not been verified. Stable availability is not guaranteed; successful submission does not mean audio was generated.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			config := &internalgen.AudioConfig{}
			changed := false
			for _, setting := range []struct {
				flag string
				set  func()
			}{
				{"format", func() { config.Format = format }},
				{"sample-rate", func() { config.SampleRate = &sampleRate }},
				{"speech-rate", func() { config.SpeechRate = &speechRate }},
				{"loudness-rate", func() { config.LoudnessRate = &loudnessRate }},
				{"pitch-rate", func() { config.PitchRate = &pitchRate }},
				{"enable-timestamp", func() { config.EnableTimestamp = &enableTimestamp }},
			} {
				if cmd.Flags().Changed(setting.flag) {
					setting.set()
					changed = true
				}
			}
			opts.AudioConfig = nil
			if changed {
				opts.AudioConfig = config
			}
			result, err := internalgen.Run(cmd.Context(), opts, runner)
			if err != nil {
				_ = common.AppendDailyErrorLog("generate-audio", err, map[string]string{
					"prompt": strings.TrimSpace(opts.Prompt),
					"model":  strings.TrimSpace(opts.Model),
				})
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&opts.Prompt, "prompt", "", "audio generation prompt")
	flags.StringVar(&opts.Model, "model", internalgen.DefaultModel, "audio model; only seedaudio_1.0 is supported; model switching is not available")
	flags.StringArrayVar(&opts.AudioPaths, "audio", nil, "local reference audio path; repeat up to 3 times; cannot combine with --image")
	flags.StringArrayVar(&opts.ImagePaths, "image", nil, "local reference image path; generation not yet verified; at most one; cannot combine with --audio")
	flags.StringVar(&format, "format", "", "output audio format: mp3, wav, pcm or ogg_opus; omitted uses the server default")
	flags.Int32Var(&sampleRate, "sample-rate", 0, "output audio sample rate in Hz")
	flags.Float64Var(&speechRate, "speech-rate", 0, "Seed Audio 1.0 speech rate; accepted range is validated by the server")
	flags.Float64Var(&loudnessRate, "loudness-rate", 0, "Seed Audio 1.0 loudness rate; accepted range is validated by the server")
	flags.Float64Var(&pitchRate, "pitch-rate", 0, "Seed Audio 1.0 pitch rate; accepted range is validated by the server")
	flags.BoolVar(&enableTimestamp, "enable-timestamp", false, "request audio timestamps")
	return cmd
}
