package generate_video

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/bytedance/sonic"
)

const (
	agentNameVideoPart        = "pippit_video_part_agent"
	partTypeData              = "data"
	partSubTypeDirectToolCall = "biz/x_data_direct_tool_call_req"
	toolNameVideoPart         = "biz/x_tool_name_video_part"
	messageRoleUser           = "user"
)

// Options is the stable command-facing request shape for generate_video.
type Options struct {
	Prompt       string
	ImagePaths   []string
	VideoPaths   []string
	DurationSec  *int
	Ratio        string
	Model        string
	Resolution   string
	GenerateType *int
}

type mediaAsset struct {
	AssetID string `json:"asset_id"`
}

type videoPartToolParam struct {
	Images       []mediaAsset `json:"images,omitempty"`
	Prompt       string       `json:"prompt"`
	DurationSec  *int         `json:"duration_sec,omitempty"`
	Ratio        string       `json:"ratio,omitempty"`
	Videos       []mediaAsset `json:"videos,omitempty"`
	Model        string       `json:"model,omitempty"`
	Resolution   string       `json:"resolution,omitempty"`
	GenerateType *int         `json:"generate_type,omitempty"`
}

type directToolCallPart struct {
	ToolName string `json:"tool_name"`
	Param    string `json:"param"`
}

type messagePart struct {
	Type    string `json:"type"`
	SubType string `json:"sub_type"`
	Data    string `json:"data"`
}

type message struct {
	Role    string        `json:"role"`
	Content []messagePart `json:"content"`
}

type submitRunResponse struct {
	Ret    string `json:"ret"`
	Errmsg string `json:"errmsg"`
	Data   struct {
		WebThreadLink string `json:"web_thread_link"`
		Run           struct {
			ThreadID string `json:"thread_id"`
			RunID    string `json:"run_id"`
		} `json:"run"`
	} `json:"data"`
}

// Result is the JSON envelope printed by `pippit-tool-cli generate_video`.
type Result struct {
	ThreadID      string   `json:"thread_id"`
	RunID         string   `json:"run_id"`
	WebThreadLink string   `json:"web_thread_link"`
	ImageAssetIDs []string `json:"image_asset_ids,omitempty"`
	VideoAssetIDs []string `json:"video_asset_ids,omitempty"`
}

func Run(ctx context.Context, opts *Options, runner *common.Runner) (*Result, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("generate_video runner client is required")
	}
	if err := ValidateOptions(opts); err != nil {
		return nil, err
	}

	imageAssetIDs, err := uploadMediaList(ctx, opts.ImagePaths, runner)
	if err != nil {
		return nil, fmt.Errorf("upload image: %w", err)
	}
	videoAssetIDs, err := uploadMediaList(ctx, opts.VideoPaths, runner)
	if err != nil {
		return nil, fmt.Errorf("upload video: %w", err)
	}

	body, err := buildSubmitRunBody(opts, imageAssetIDs, videoAssetIDs)
	if err != nil {
		return nil, err
	}

	var resp submitRunResponse
	if err := runner.Client.SendRequest(ctx, submitRunPath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("submit_run request failed: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "unknown error"
		}
		return nil, fmt.Errorf("submit_run failed: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
	}
	if resp.Data.Run.ThreadID == "" {
		return nil, fmt.Errorf("submit_run response missing data.run.thread_id")
	}
	if resp.Data.Run.RunID == "" {
		return nil, fmt.Errorf("submit_run response missing data.run.run_id")
	}

	return &Result{
		ThreadID:      resp.Data.Run.ThreadID,
		RunID:         resp.Data.Run.RunID,
		WebThreadLink: resp.Data.WebThreadLink,
		ImageAssetIDs: imageAssetIDs,
		VideoAssetIDs: videoAssetIDs,
	}, nil
}

func ValidateOptions(_ *Options) error {
	return nil
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

func buildSubmitRunBody(opts *Options, imageAssetIDs []string, videoAssetIDs []string) (map[string]any, error) {
	param := videoPartToolParam{
		Images:       assetRefs(imageAssetIDs),
		Prompt:       strings.TrimSpace(opts.Prompt),
		DurationSec:  opts.DurationSec,
		Ratio:        strings.TrimSpace(opts.Ratio),
		Videos:       assetRefs(videoAssetIDs),
		Model:        strings.TrimSpace(opts.Model),
		Resolution:   strings.TrimSpace(opts.Resolution),
		GenerateType: opts.GenerateType,
	}
	paramJSON, err := sonic.MarshalString(param)
	if err != nil {
		return nil, fmt.Errorf("encode video part param: %w", err)
	}

	toolCallJSON, err := sonic.MarshalString(directToolCallPart{
		ToolName: toolNameVideoPart,
		Param:    paramJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("encode direct tool call: %w", err)
	}

	return map[string]any{
		"agent_name": agentNameVideoPart,
		"message": message{
			Role: messageRoleUser,
			Content: []messagePart{
				{
					Type:    partTypeData,
					SubType: partSubTypeDirectToolCall,
					Data:    toolCallJSON,
				},
			},
		},
	}, nil
}

func assetRefs(assetIDs []string) []mediaAsset {
	if len(assetIDs) == 0 {
		return nil
	}
	refs := make([]mediaAsset, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		refs = append(refs, mediaAsset{AssetID: assetID})
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
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func submitRunPath(runner *common.Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.GenerateVideoSubmitRun != "" {
		return runner.Config.Paths.GenerateVideoSubmitRun
	}
	return config.AgentSubmitRunPath
}
