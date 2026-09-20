package generate_audio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	internalgen "github.com/Pippit-dev/pippit-cli/internal/generate_audio"
	"github.com/spf13/cobra"
)

func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var prompt, model, input, filePath, format string
	var sampleRate int32
	var speechRate, loudnessRate, pitchRate float64
	var enableTimestamp bool
	var references []internalgen.LocalReference
	cmd := &cobra.Command{
		Use:   "generate-audio",
		Short: "Generate audio with model parameters validated by the service",
		Long: "Generate audio using convenience flags or an audio_part_tool_param JSON object via --input or --file (use - for stdin). Models and parameter combinations are validated by the service. Explicit values are passed unchanged.\n\n" +
			"JSON mode does not add a model default. Calls without JSON retain seedaudio_1.0 as a compatibility default, not a model allowlist. Legacy output flags only populate audio_config; use JSON for other configurations and modes.\n\n" +
			"Conflicting JSON fields and explicit flags are rejected. Local audio/image/video references are appended after JSON references in flag order. Reference image submission is connected, but successful generation has not been verified.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := cmd.Flags()
			if flags.Changed("input") && flags.Changed("file") {
				return fmt.Errorf("--input 和 --file 不能同时使用")
			}
			jsonMode := flags.Changed("input") || flags.Changed("file")
			params := make(map[string]json.RawMessage)
			if jsonMode {
				if flags.Changed("file") && strings.TrimSpace(filePath) == "" {
					return fmt.Errorf("--file 不能为空")
				}
				var err error
				params, err = readParameters(input, filePath, cmd.InOrStdin())
				if err != nil {
					return err
				}
			} else if flags.NFlag() == 0 {
				return fmt.Errorf("请提供 --prompt 或 --input/--file 音频参数")
			}
			for _, setting := range []struct{ key, value string }{{"prompt", prompt}, {"model", model}} {
				if flags.Changed(setting.key) {
					if err := addFlag(params, setting.key, setting.key, setting.value); err != nil {
						return err
					}
				}
			}
			if !jsonMode && !flags.Changed("model") {
				params["model"] = json.RawMessage(`"` + internalgen.DefaultModel + `"`)
			}
			settings := []struct {
				flag, key string
				value     any
			}{
				{"format", "format", format}, {"sample-rate", "sample_rate", sampleRate},
				{"speech-rate", "speech_rate", speechRate}, {"loudness-rate", "loudness_rate", loudnessRate},
				{"pitch-rate", "pitch_rate", pitchRate}, {"enable-timestamp", "enable_timestamp", enableTimestamp},
			}
			var config map[string]json.RawMessage
			for _, setting := range settings {
				if !flags.Changed(setting.flag) {
					continue
				}
				if config == nil {
					config = make(map[string]json.RawMessage)
					if raw, exists := params["audio_config"]; exists {
						if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) || json.Unmarshal(raw, &config) != nil {
							return fmt.Errorf("--%s 需要合并 audio_config，但 JSON audio_config 不是对象", setting.flag)
						}
					}
				}
				if err := addFlag(config, setting.key, setting.flag, setting.value); err != nil {
					return fmt.Errorf("audio_config: %w", err)
				}
			}
			if config != nil {
				raw, err := json.Marshal(config)
				if err != nil {
					return err
				}
				params["audio_config"] = raw
			}
			result, err := internalgen.Run(cmd.Context(), &internalgen.Options{Parameters: params, LocalReferences: references}, runner)
			if err != nil {
				_ = common.AppendDailyErrorLog("generate-audio", err, nil)
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&input, "input", "", "audio_part_tool_param JSON object; cannot combine with --file")
	flags.StringVar(&filePath, "file", "", "audio parameter JSON file, or - for stdin; cannot combine with --input")
	flags.StringVar(&prompt, "prompt", "", "audio prompt passed unchanged; conflicts with JSON prompt")
	flags.StringVar(&model, "model", internalgen.DefaultModel, "model passed unchanged; service validates support; default only applies without JSON input")
	for _, kind := range []string{"audio", "image", "video"} {
		flags.Var(&referenceFlag{kind: kind, references: &references}, kind, "local reference "+kind+" path; repeat to append after JSON references, in flag order")
	}
	flags.StringVar(&format, "format", "", "legacy audio_config.format; passed unchanged, support is validated by the service")
	flags.Int32Var(&sampleRate, "sample-rate", 0, "legacy audio_config.sample_rate in Hz")
	flags.Float64Var(&speechRate, "speech-rate", 0, "legacy audio_config.speech_rate")
	flags.Float64Var(&loudnessRate, "loudness-rate", 0, "legacy audio_config.loudness_rate")
	flags.Float64Var(&pitchRate, "pitch-rate", 0, "legacy audio_config.pitch_rate")
	flags.BoolVar(&enableTimestamp, "enable-timestamp", false, "legacy audio_config.enable_timestamp")
	return cmd
}

// Each flag shares one ordered list, including when different media types are interleaved.
type referenceFlag struct {
	kind       string
	references *[]internalgen.LocalReference
}

func (f *referenceFlag) Set(value string) error {
	*f.references = append(*f.references, internalgen.LocalReference{Type: f.kind, Path: value})
	return nil
}
func (f *referenceFlag) Type() string { return "stringArray" }
func (f *referenceFlag) String() string {
	values := make([]string, 0)
	for _, ref := range *f.references {
		if ref.Type == f.kind {
			values = append(values, ref.Path)
		}
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}
