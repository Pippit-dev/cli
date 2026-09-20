package generate_audio

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const DefaultModel = "seedaudio_1.0"

// Options describes a Seed Audio 1.0 request. Unset output settings use server defaults.
type Options struct {
	Prompt      string
	Model       string
	AudioPaths  []string
	ImagePaths  []string
	AudioConfig *AudioConfig
}

type AudioConfig struct {
	Format          string   `json:"format,omitempty"`
	SampleRate      *int32   `json:"sample_rate,omitempty"`
	SpeechRate      *float64 `json:"speech_rate,omitempty"`
	LoudnessRate    *float64 `json:"loudness_rate,omitempty"`
	PitchRate       *float64 `json:"pitch_rate,omitempty"`
	EnableTimestamp *bool    `json:"enable_timestamp,omitempty"`
}

type audioReference struct {
	Type          string `json:"type"`
	PippitAssetID string `json:"pippit_asset_id"`
}

type audioPartToolParam struct {
	Prompt      string           `json:"prompt"`
	Model       string           `json:"model"`
	AudioConfig *AudioConfig     `json:"audio_config,omitempty"`
	References  []audioReference `json:"references,omitempty"`
}

func Run(ctx context.Context, opts *Options, runner *common.Runner) (*common.SubmitRunResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("generate-audio 运行器客户端缺失")
	}
	if err := ValidateOptions(opts); err != nil {
		return nil, err
	}
	refs := make([]audioReference, 0, len(opts.AudioPaths)+len(opts.ImagePaths))
	for _, group := range []struct {
		kind  string
		paths []string
	}{{"audio", opts.AudioPaths}, {"image", opts.ImagePaths}} {
		for _, path := range group.paths {
			expanded, err := common.ExpandPath(path)
			if err != nil {
				return nil, err
			}
			upload, err := common.UploadFile(ctx, common.UploadFileOptions{Path: expanded}, runner)
			if err != nil {
				return nil, fmt.Errorf("上传音频生成参考素材失败: %w", err)
			}
			refs = append(refs, audioReference{Type: group.kind, PippitAssetID: upload.AssetID})
		}
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = DefaultModel
	}
	var config *AudioConfig
	if opts.AudioConfig != nil {
		value := *opts.AudioConfig
		value.Format = strings.ToLower(strings.TrimSpace(value.Format))
		config = &value
	}
	return common.SubmitRun(ctx, "generate-audio", map[string]any{
		"agent_name": "pippit_audio_part_agent",
		"message":    strings.TrimSpace(opts.Prompt),
		"audio_part_tool_param": audioPartToolParam{
			Prompt: strings.TrimSpace(opts.Prompt), Model: model,
			AudioConfig: config, References: refs,
		},
	}, runner)
}

func ValidateOptions(opts *Options) error {
	if opts == nil || strings.TrimSpace(opts.Prompt) == "" {
		return fmt.Errorf("缺少必填参数 --prompt")
	}
	if model := strings.TrimSpace(opts.Model); model != "" && model != DefaultModel {
		return fmt.Errorf("--model 仅支持 %s", DefaultModel)
	}
	if len(opts.AudioPaths) > 0 && len(opts.ImagePaths) > 0 {
		return fmt.Errorf("--audio 与 --image 不能混用")
	}
	if len(opts.AudioPaths) > 3 || len(opts.ImagePaths) > 1 {
		return fmt.Errorf("最多支持 3 个参考音频或 1 张参考图片")
	}
	for _, group := range []struct {
		flag    string
		paths   []string
		allowed []string
	}{
		{"--audio", opts.AudioPaths, []string{".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg", ".opus"}},
		{"--image", opts.ImagePaths, []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg"}},
	} {
		allowed := common.StringSet(group.allowed)
		for _, path := range group.paths {
			ext := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
			if _, ok := allowed[ext]; !ok {
				return fmt.Errorf("%s 不支持文件后缀 %q；支持：%s", group.flag, ext, strings.Join(group.allowed, ", "))
			}
		}
	}
	if config := opts.AudioConfig; config != nil {
		switch strings.ToLower(strings.TrimSpace(config.Format)) {
		case "", "mp3", "wav", "pcm", "ogg_opus":
		default:
			return fmt.Errorf("--format 仅支持 mp3、wav、pcm 或 ogg_opus")
		}
		if config.SampleRate != nil && *config.SampleRate <= 0 {
			return fmt.Errorf("--sample-rate 必须为正整数")
		}
		for _, setting := range []struct {
			flag  string
			value *float64
		}{{"--speech-rate", config.SpeechRate}, {"--loudness-rate", config.LoudnessRate}, {"--pitch-rate", config.PitchRate}} {
			if setting.value != nil && (math.IsNaN(*setting.value) || math.IsInf(*setting.value, 0)) {
				return fmt.Errorf("%s 必须为有限数值", setting.flag)
			}
		}
	}
	return nil
}
