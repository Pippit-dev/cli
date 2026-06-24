package generate_video

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const (
	agentNameVideoPart = "pippit_video_part_agent"
)

var (
	allowedImageExtensionList = []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg"}
	allowedVideoExtensionList = []string{".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mkv", ".m4v"}
	allowedAudioExtensionList = []string{".mp3", ".wav"}
	allowedImageExtensions    = makeExtensionSet(allowedImageExtensionList)
	allowedVideoExtensions    = makeExtensionSet(allowedVideoExtensionList)
	allowedAudioExtensions    = makeExtensionSet(allowedAudioExtensionList)
)

// Options is the stable command-facing request shape for generate-video.
type Options struct {
	Prompt      string
	ImagePaths  []string
	VideoPaths  []string
	AudioPaths  []string
	DurationSec *int
	Ratio       string
	Model       string
	Resolution  string
}

type mediaAsset struct {
	PippitAssetID string `json:"pippit_asset_id"`
}

type videoPartToolParam struct {
	Images      []mediaAsset `json:"images,omitempty"`
	Prompt      string       `json:"prompt"`
	DurationSec *int         `json:"duration_sec,omitempty"`
	Ratio       string       `json:"ratio,omitempty"`
	Videos      []mediaAsset `json:"videos,omitempty"`
	Audios      []mediaAsset `json:"audios,omitempty"`
	Model       string       `json:"model,omitempty"`
	Resolution  string       `json:"resolution,omitempty"`
}

// Result is the JSON envelope printed by `pippit-tool-cli generate-video`.
type Result struct {
	ThreadID      string `json:"thread_id"`
	RunID         string `json:"run_id"`
	WebThreadLink string `json:"web_thread_link"`
}

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

	var resp common.SubmitRunResponse
	if err := runner.Client.SendRequest(ctx, common.SubmitRunPath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("提交 generate-video 请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, common.NewLogIDError(fmt.Sprintf("generate-video 请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg), resp.LogID)
	}
	if resp.Data.Run.ThreadID == "" {
		return nil, fmt.Errorf("generate-video 响应缺少 data.run.thread_id")
	}
	if resp.Data.Run.RunID == "" {
		return nil, fmt.Errorf("generate-video 响应缺少 data.run.run_id")
	}

	return &Result{
		ThreadID:      resp.Data.Run.ThreadID,
		RunID:         resp.Data.Run.RunID,
		WebThreadLink: resp.Data.WebThreadLink,
	}, nil
}

func ValidateOptions(opts *Options) error {
	if opts == nil {
		return fmt.Errorf("缺少必填参数 --prompt")
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return fmt.Errorf("缺少必填参数 --prompt")
	}
	if err := validateMediaExtensions("图片", opts.ImagePaths, allowedImageExtensions, allowedImageExtensionList); err != nil {
		return err
	}
	if err := validateMediaExtensions("视频", opts.VideoPaths, allowedVideoExtensions, allowedVideoExtensionList); err != nil {
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

func makeExtensionSet(list []string) map[string]struct{} {
	set := make(map[string]struct{}, len(list))
	for _, ext := range list {
		set[ext] = struct{}{}
	}
	return set
}

func uploadMediaList(ctx context.Context, paths []string, runner *common.Runner) ([]string, error) {
	assetIDs := make([]string, 0, len(paths))
	for _, path := range paths {
		expanded, err := expandPath(path)
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
	param := videoPartToolParam{
		Images:      assetRefs(imageAssetIDs),
		Prompt:      strings.TrimSpace(opts.Prompt),
		DurationSec: opts.DurationSec,
		Ratio:       strings.TrimSpace(opts.Ratio),
		Videos:      assetRefs(videoAssetIDs),
		Audios:      assetRefs(audioAssetIDs),
		Model:       strings.TrimSpace(opts.Model),
		Resolution:  strings.TrimSpace(opts.Resolution),
	}

	return map[string]any{
		"agent_name":            agentNameVideoPart,
		"message":               strings.TrimSpace(opts.Prompt),
		"video_part_tool_param": param,
	}
}

func assetRefs(assetIDs []string) []mediaAsset {
	if len(assetIDs) == 0 {
		return nil
	}
	refs := make([]mediaAsset, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		refs = append(refs, mediaAsset{PippitAssetID: assetID})
	}
	return refs
}

func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("解析用户主目录失败: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
