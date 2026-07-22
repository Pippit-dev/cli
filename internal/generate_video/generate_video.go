package generate_video

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const (
	maxReferenceImages = 9
	maxReferenceVideos = 3
	maxReferenceAudios = 3
)

var (
	allowedImageExtensionList = []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg"}
	allowedAudioExtensionList = []string{".mp3", ".wav"}
	allowedImageExtensions    = common.StringSet(allowedImageExtensionList)
	allowedAudioExtensions    = common.StringSet(allowedAudioExtensionList)
)

// Options is the stable command-facing request shape for generate-video.
type Options struct {
	Prompt       string
	ImagePaths   []string
	VideoPaths   []string
	AudioPaths   []string
	DurationSec  *int
	Ratio        string
	Model        string
	Resolution   string
	GenerateType *int64
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

	imageAssetIDs, err := uploadMediaList(ctx, opts.ImagePaths, runner)
	if err != nil {
		return nil, fmt.Errorf("上传图片失败: %w", err)
	}
	videoAssetIDs, err := uploadMediaList(ctx, opts.VideoPaths, runner)
	if err != nil {
		return nil, fmt.Errorf("上传视频失败: %w", err)
	}
	audioAssetIDs, err := uploadMediaList(ctx, opts.AudioPaths, runner)
	if err != nil {
		return nil, fmt.Errorf("上传音频失败: %w", err)
	}

	body := buildSubmitRunBody(opts, imageAssetIDs, videoAssetIDs, audioAssetIDs)
	return common.SubmitRun(ctx, "generate-video", body, runner)
}

func ValidateOptions(opts *Options) error {
	if opts == nil {
		return fmt.Errorf("缺少必填参数 --prompt")
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return fmt.Errorf("缺少必填参数 --prompt")
	}
	if len(opts.ImagePaths) > maxReferenceImages {
		return fmt.Errorf("参考图片最多支持 %d 个，当前传入 %d 个", maxReferenceImages, len(opts.ImagePaths))
	}
	if len(opts.VideoPaths) > maxReferenceVideos {
		return fmt.Errorf("参考视频最多支持 %d 个，当前传入 %d 个", maxReferenceVideos, len(opts.VideoPaths))
	}
	if len(opts.AudioPaths) > maxReferenceAudios {
		return fmt.Errorf("参考音频最多支持 %d 个，当前传入 %d 个", maxReferenceAudios, len(opts.AudioPaths))
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

func uploadMediaList(ctx context.Context, paths []string, runner *common.Runner) ([]string, error) {
	assetIDs := make([]string, 0, len(paths))
	for _, path := range paths {
		expanded, err := common.ExpandPath(path)
		if err != nil {
			return nil, err
		}
		result, err := common.UploadFile(ctx, common.UploadFileOptions{Path: expanded}, runner)
		if err != nil {
			return nil, err
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
	}

	return map[string]any{
		"agent_name":            common.AgentNameVideoPart,
		"message":               prompt,
		"video_part_tool_param": param,
	}
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
