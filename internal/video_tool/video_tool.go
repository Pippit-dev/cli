package video_tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const (
	toolVideoSuperResolution = "video_super_resolution"
	toolEraseVideoSubtitle   = "erase_video_subtitle"
	messageSuperResolution   = "提升视频清晰度"
	messageEraseSubtitle     = "擦除视频字幕"
)

// SuperResolutionOptions is the command-facing request shape for video-super-resolution.
type SuperResolutionOptions struct {
	VideoPath        string
	ToolVersion      string
	OutputResolution string
}

// EraseSubtitleOptions is the command-facing request shape for erase-video-subtitle.
type EraseSubtitleOptions struct {
	VideoPath string
}

// Result is the JSON envelope printed by both video tool commands.
type Result = common.SubmitRunResult

// RunSuperResolution uploads one video and submits a video super-resolution run.
func RunSuperResolution(ctx context.Context, opts *SuperResolutionOptions, runner *common.Runner) (*Result, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("video-super-resolution 运行器客户端缺失")
	}
	if err := ValidateSuperResolutionOptions(opts); err != nil {
		return nil, err
	}

	assetID, err := uploadVideo(ctx, opts.VideoPath, runner)
	if err != nil {
		return nil, err
	}
	return common.SubmitRun(ctx, "video-super-resolution", buildSuperResolutionSubmitRunBody(opts, assetID), runner)
}

// RunEraseSubtitle uploads one video and submits a subtitle-erasing run.
func RunEraseSubtitle(ctx context.Context, opts *EraseSubtitleOptions, runner *common.Runner) (*Result, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("erase-video-subtitle 运行器客户端缺失")
	}
	if err := ValidateEraseSubtitleOptions(opts); err != nil {
		return nil, err
	}

	assetID, err := uploadVideo(ctx, opts.VideoPath, runner)
	if err != nil {
		return nil, err
	}
	return common.SubmitRun(ctx, "erase-video-subtitle", buildEraseSubtitleSubmitRunBody(assetID), runner)
}

// ValidateSuperResolutionOptions validates only command shape; semantic enums remain service-owned.
func ValidateSuperResolutionOptions(opts *SuperResolutionOptions) error {
	if opts == nil || strings.TrimSpace(opts.VideoPath) == "" {
		return fmt.Errorf("缺少必填参数 --video")
	}
	if strings.TrimSpace(opts.OutputResolution) == "" {
		return fmt.Errorf("缺少必填参数 --output-resolution")
	}
	return common.ValidateVideoExtensions([]string{opts.VideoPath})
}

// ValidateEraseSubtitleOptions validates the command shape.
func ValidateEraseSubtitleOptions(opts *EraseSubtitleOptions) error {
	if opts == nil || strings.TrimSpace(opts.VideoPath) == "" {
		return fmt.Errorf("缺少必填参数 --video")
	}
	return common.ValidateVideoExtensions([]string{opts.VideoPath})
}

func uploadVideo(ctx context.Context, path string, runner *common.Runner) (string, error) {
	expanded, err := common.ExpandPath(path)
	if err != nil {
		return "", err
	}
	result, err := common.UploadFile(ctx, common.UploadFileOptions{Path: expanded}, runner)
	if err != nil {
		return "", fmt.Errorf("上传视频失败: %w", err)
	}
	return result.AssetID, nil
}

func buildSuperResolutionSubmitRunBody(opts *SuperResolutionOptions, assetID string) map[string]any {
	return map[string]any{
		"agent_name": common.AgentNameVideoPart,
		"message":    messageSuperResolution,
		"video_part_tool_param": common.VideoPartToolParam{
			MiniToolParam: &common.VideoMiniToolParam{
				ToolName: toolVideoSuperResolution,
				ToolParam: &common.VideoMiniToolPayload{
					VideoSuperResolutionToolParam: &common.VideoSuperResolutionToolParam{
						ToolVersion:      strings.TrimSpace(opts.ToolVersion),
						Video:            &common.MediaAsset{PippitAssetID: assetID},
						OutputResolution: strings.TrimSpace(opts.OutputResolution),
					},
				},
			},
		},
	}
}

func buildEraseSubtitleSubmitRunBody(assetID string) map[string]any {
	return map[string]any{
		"agent_name": common.AgentNameVideoPart,
		"message":    messageEraseSubtitle,
		"video_part_tool_param": common.VideoPartToolParam{
			MiniToolParam: &common.VideoMiniToolParam{
				ToolName: toolEraseVideoSubtitle,
				ToolParam: &common.VideoMiniToolPayload{
					EraseVideoSubtitleToolParam: &common.EraseVideoSubtitleToolParam{
						Video: &common.MediaAsset{PippitAssetID: assetID},
					},
				},
			},
		},
	}
}
