package generate_video

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

var (
	allowedImageExtensionList = []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg"}
	allowedAudioExtensionList = []string{".mp3", ".wav"}
	allowedImageExtensions    = common.StringSet(allowedImageExtensionList)
	allowedAudioExtensions    = common.StringSet(allowedAudioExtensionList)
)

// Options is the stable command-facing request shape for generate-video.
type Options struct {
	Source       string
	Prompt       string
	ImagePaths   []string
	VideoPaths   []string
	AudioPaths   []string
	DurationSec  *int
	Ratio        string
	Model        string
	Resolution   string
	GenerateType *int64
	TaskType     string
	Seed         *int64
	Draft        *bool
	DraftTaskID  string
}

// Result is the JSON envelope printed by `pippit-tool-cli generate-video`.
type Result = common.SubmitRunResult

func Run(ctx context.Context, opts *Options, runner *common.Runner) (*Result, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("generate-video 运行器客户端缺失")
	}
	if err := ValidateOptions(opts); err != nil {
		return nil, err
	}

	imageAssetIDs, err := uploadMediaList(ctx, "张参考图片", opts.ImagePaths, runner)
	if err != nil {
		return nil, err
	}
	videoAssetIDs, err := uploadMediaList(ctx, "个参考视频", opts.VideoPaths, runner)
	if err != nil {
		return nil, err
	}
	audioAssetIDs, err := uploadMediaList(ctx, "段参考音频", opts.AudioPaths, runner)
	if err != nil {
		return nil, err
	}

	body := buildSubmitRunBody(opts, imageAssetIDs, videoAssetIDs, audioAssetIDs)
	// Business attribution belongs to this command, regardless of the selected model.
	return common.SubmitRunWithBabiParam(ctx, "generate-video", body, runner, map[string]string{
		"scene_lv1":  "ai_agent",
		"scene_lv2":  "front_tool",
		"tool_id":    "instant_video",
		"tab_name":   "other",
		"edit_type":  "instant_video",
		"enter_from": "skill",
	})
}

func ValidateOptions(opts *Options) error {
	if opts == nil {
		return fmt.Errorf("缺少必填参数 --prompt")
	}
	if strings.TrimSpace(opts.DraftTaskID) == "" && strings.TrimSpace(opts.Prompt) == "" {
		return fmt.Errorf("缺少必填参数 --prompt")
	}
	if err := validateMediaExtensions("图片", opts.ImagePaths, allowedImageExtensions, allowedImageExtensionList); err != nil {
		return err
	}
	if err := common.ValidateVideoExtensions(opts.VideoPaths); err != nil {
		return err
	}
	if err := validateMediaExtensions("音频", opts.AudioPaths, allowedAudioExtensions, allowedAudioExtensionList); err != nil {
		return err
	}
	return nil
}

func validateMediaExtensions(kind string, paths []string, allowed map[string]struct{}, allowedList []string) error {
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
		if _, ok := allowed[ext]; !ok {
			return fmt.Errorf("不支持的%s文件后缀 %q，文件：%q；支持的后缀：%s", kind, ext, path, strings.Join(allowedList, ", "))
		}
	}
	return nil
}

func uploadMediaList(ctx context.Context, mediaLabel string, paths []string, runner *common.Runner) ([]string, error) {
	assetIDs := make([]string, 0, len(paths))
	for i, path := range paths {
		expanded, err := common.ExpandPath(path)
		if err != nil {
			return nil, fmt.Errorf("第%d%s %q 路径解析失败: %w", i+1, mediaLabel, filepath.Base(path), err)
		}
		result, err := common.UploadFile(ctx, common.UploadFileOptions{Path: expanded}, runner)
		if err != nil {
			return nil, fmt.Errorf("第%d%s %q 上传失败: %w", i+1, mediaLabel, filepath.Base(path), err)
		}
		assetIDs = append(assetIDs, result.AssetID)
	}
	return assetIDs, nil
}

func buildSubmitRunBody(opts *Options, imageAssetIDs []string, videoAssetIDs []string, audioAssetIDs []string) map[string]any {
	prompt := strings.TrimSpace(opts.Prompt)
	param := common.VideoPartToolParam{
		Images:       assetRefs(imageAssetIDs),
		Prompt:       &prompt,
		DurationSec:  opts.DurationSec,
		Ratio:        strings.TrimSpace(opts.Ratio),
		Videos:       assetRefs(videoAssetIDs),
		Audios:       assetRefs(audioAssetIDs),
		Model:        strings.TrimSpace(opts.Model),
		Resolution:   strings.TrimSpace(opts.Resolution),
		GenerateType: opts.GenerateType,
		TaskType:     strings.TrimSpace(opts.TaskType),
		Seed:         opts.Seed,
		Draft:        opts.Draft,
		DraftTaskID:  strings.TrimSpace(opts.DraftTaskID),
	}

	return common.WithSubmitRunSource(map[string]any{
		"agent_name":            common.AgentNameVideoPart,
		"call_source":           "pippit_cli",
		"message":               prompt,
		"video_part_tool_param": param,
	}, opts.Source)
}

func assetRefs(assetIDs []string) []*common.MediaAsset {
	if len(assetIDs) == 0 {
		return nil
	}
	refs := make([]*common.MediaAsset, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		refs = append(refs, &common.MediaAsset{PippitAssetID: assetID})
	}
	return refs
}
