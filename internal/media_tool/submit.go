package media_tool

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/commandnames"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/space"
)

type Options struct {
	Tool            string
	AssetID         string
	Quality         string
	BackgroundColor string
	Source          string
}

type Result struct {
	common.SubmitRunResult
	Tool             string `json:"tool"`
	Status           string `json:"status"`
	ErrorMessage     string `json:"error_message,omitempty"`
	LogID            string `json:"log_id,omitempty"`
	ResponseThreadID string `json:"response_thread_id,omitempty"`
	ResponseRunID    string `json:"response_run_id,omitempty"`
}

type Part struct {
	Type    string `json:"type"`
	SubType string `json:"sub_type"`
	Data    string `json:"data"`
}

type Message struct {
	Role     string `json:"role"`
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id"`
	Content  []Part `json:"content"`
}

type Request struct {
	AgentName string  `json:"agent_name"`
	Message   Message `json:"message"`
	assetID   string
}

func (r *Request) SpaceReferences() space.References {
	return space.References{ThreadID: r.Message.ThreadID, RunID: r.Message.RunID, AssetIDs: []string{r.assetID}}
}

func BuildRequest(opts Options) (*Request, error) {
	opts.AssetID = strings.TrimSpace(opts.AssetID)
	if opts.AssetID == "" {
		return nil, fmt.Errorf("缺少必填参数 --asset-id；先使用 upload-file 上传素材")
	}
	var agent, subtype string
	var payload any
	switch opts.Tool {
	case commandnames.ImageSuperResolution:
		if opts.BackgroundColor != "" {
			return nil, fmt.Errorf("图片超分不支持 background-color")
		}
		quality := strings.TrimSpace(opts.Quality)
		if quality != "" && quality != "LQ" && quality != "MQ" && quality != "HQ" {
			return nil, fmt.Errorf("--quality 仅支持 LQ、MQ、HQ")
		}
		args := map[string]string{}
		if quality != "" {
			args["model_quality"] = quality
		}
		agent, subtype = "pippit_novel_agent_cn_v2", "biz/x_data_novel_raw_image_gen"
		payload = struct {
			PromptTemplate string            `json:"prompt_template"`
			Args           map[string]string `json:"args"`
			AssetIDs       []string          `json:"pippit_asset_ids"`
			ImageCount     int               `json:"image_count"`
		}{"tool/image_super_resolution", args, []string{opts.AssetID}, 1}
	case commandnames.VideoMatting:
		if opts.Quality != "" {
			return nil, fmt.Errorf("视频抠像不支持 quality")
		}
		color := strings.TrimSpace(opts.BackgroundColor)
		if color != "" && color != "black" && color != "green" {
			return nil, fmt.Errorf("--background-color 仅支持 black、green；输出为纯色背景 MP4")
		}
		agent, subtype = "pippit_novel_video_part_agent", "biz/x_data_direct_tool_call_req"
		param, err := json.Marshal(map[string]any{"mini_tool_param": map[string]any{
			"tool_name": "video_matting", "tool_param": map[string]any{"video_matting_tool_param": map[string]any{
				"video": common.MediaAsset{PippitAssetID: opts.AssetID}, "background_color": color,
			}},
		}})
		if err != nil {
			return nil, err
		}
		payload = struct {
			ToolName string `json:"tool_name"`
			Param    string `json:"param"`
		}{"biz/x_tool_name_video_part", string(param)}
	default:
		return nil, fmt.Errorf("不支持的工具 %q", opts.Tool)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ids := make([]byte, 32)
	if _, err := rand.Read(ids); err != nil {
		return nil, fmt.Errorf("生成任务标识失败: %w", err)
	}
	for _, offset := range []int{0, 16} {
		ids[offset+6] = (ids[offset+6] & 0x0f) | 0x40
		ids[offset+8] = (ids[offset+8] & 0x3f) | 0x80
	}
	uuid := func(b []byte) string { return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]) }
	return &Request{AgentName: agent, Message: Message{Role: "user", ThreadID: uuid(ids[:16]), RunID: uuid(ids[16:]), Content: []Part{{Type: "data", SubType: subtype, Data: string(data)}}}, assetID: opts.AssetID}, nil
}

// Submit never retries a potentially billable request. beforeSend publishes the
// recovery identifiers before the network call, including if the process exits.
func Submit(ctx context.Context, opts Options, runner *common.Runner, beforeSend func(*Result) error) (*Result, error) {
	req, err := BuildRequest(opts)
	if err != nil {
		return nil, err
	}
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("工具运行器客户端缺失")
	}
	if session := common.SpaceSession(ctx); session != nil {
		if _, err := session.Resolve(req.SpaceReferences()); err != nil {
			return nil, err
		}
	}
	result := &Result{SubmitRunResult: common.SubmitRunResult{TeamID: common.SpaceTeamID(ctx), ThreadID: req.Message.ThreadID, RunID: req.Message.RunID}, Tool: opts.Tool, Status: "submitting"}
	if beforeSend != nil {
		if err := beforeSend(result); err != nil {
			return nil, err
		}
	}
	path := config.CanvasSubmitRunPath
	query := url.Values{}
	if source := strings.TrimSpace(opts.Source); source != "" {
		query.Set("platform", source)
	}
	// Both operations use the novel agent tool workflow. Carry business labels
	// like the existing CLI video entry; identity is supplied only by the server.
	babi, err := json.Marshal(map[string]string{
		"scene_lv1": "ai_agent", "scene_lv2": "front_tool", "tool_id": "novel",
		"tab_name": "other", "edit_type": "novel", "enter_from": "skill",
	})
	if err != nil {
		return nil, err
	}
	query.Set("babi_param", string(babi))
	path += "?" + query.Encode()
	var resp common.SubmitRunResponse
	if err = runner.Client.SendRequest(ctx, path, req, &resp); err != nil {
		var httpErr *common.HTTPResponseError
		if errors.As(err, &httpErr) {
			result.LogID = httpErr.LogID()
			if httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden {
				result.Status = "rejected"
				result.ErrorMessage = fmt.Sprintf("提交被拒绝：%v", err)
				return result, common.NewLogIDError(result.ErrorMessage, result.LogID)
			}
		}
		result.Status = "submission_unknown"
		result.ErrorMessage = fmt.Sprintf("提交结果不明：%v；请查询原 thread/run，不要重复提交", err)
		return result, fmt.Errorf("%s", result.ErrorMessage)
	}
	result.LogID = resp.LogID
	// An internal business error can arrive after the server persisted a Run.
	// Keep its recovery IDs instead of implying that resubmission is safe.
	if resp.Ret == "1" {
		result.Status = "submission_unknown"
		result.ErrorMessage = fmt.Sprintf("提交结果不明：ret=%s errmsg=%s；请查询原 thread/run，不要重复提交", resp.Ret, resp.Errmsg)
		return result, common.NewLogIDError(result.ErrorMessage, result.LogID)
	}
	if resp.Ret != "0" {
		result.Status = "rejected"
		result.ErrorMessage = fmt.Sprintf("提交被拒绝: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
		return result, common.NewLogIDError(result.ErrorMessage, resp.LogID)
	}
	if resp.Data.Run.ThreadID != req.Message.ThreadID || resp.Data.Run.RunID != req.Message.RunID {
		result.Status = "submission_unknown"
		result.ResponseThreadID = resp.Data.Run.ThreadID
		result.ResponseRunID = resp.Data.Run.RunID
		result.ErrorMessage = "响应任务标识缺失或不一致；保留原 thread/run 查询，不要重新提交"
		return result, fmt.Errorf("%s", result.ErrorMessage)
	}
	result.Status = "submitted"
	result.WebThreadLink = resp.Data.WebThreadLink
	return result, nil
}
