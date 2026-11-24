package short_drama

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

// SubmitRunOptions is the stable command-facing request shape for short drama run submission.
type SubmitRunOptions struct {
	Source   string   `json:"platform,omitempty"`
	Message  string   `json:"message"`
	ThreadID string   `json:"thread_id,omitempty"`
	AssetIDs []string `json:"asset_ids,omitempty"`
}

type SubmitRunResult struct {
	common.SubmitRunResult
	ProjectID        string `json:"project_id,omitempty"`
	SubmissionStatus string `json:"submission_status,omitempty"`
	Warning          string `json:"warning,omitempty"`
}

func SubmitRun(ctx context.Context, opts *SubmitRunOptions, runner *common.Runner) (*SubmitRunResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("submit_run 运行器客户端缺失")
	}

	body := map[string]any{
		"message": opts.Message,
	}
	if opts.ThreadID != "" {
		body["thread_id"] = opts.ThreadID
	}
	if len(opts.AssetIDs) > 0 {
		body["asset_ids"] = opts.AssetIDs
	}
	body["agent_name"] = "pippit_nest_novel_agent"
	body = common.WithSubmitRunSource(body, opts.Source)

	var resp struct {
		common.SubmitRunResponse
		Data struct {
			common.SubmitRunResponseData
			Run struct {
				common.SubmitRunResponseRun
				State int `json:"state"`
			} `json:"run"`
			Thread struct {
				ThreadID  string      `json:"thread_id"`
				ProjectID json.Number `json:"project_id"`
			} `json:"thread"`
		} `json:"data"`
	}
	if err := runner.Client.SendRequest(ctx, common.SubmitRunPath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("提交 short_drama 请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, fmt.Errorf("short_drama 请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
	}
	if resp.Data.Run.ThreadID == "" {
		return nil, fmt.Errorf("short_drama 响应缺少 data.run.thread_id")
	}
	if resp.Data.Run.RunID == "" {
		return nil, fmt.Errorf("short_drama 响应缺少 data.run.run_id")
	}
	result := &SubmitRunResult{SubmitRunResult: common.SubmitRunResult{
		TeamID:        common.SpaceTeamID(ctx),
		ThreadID:      resp.Data.Run.ThreadID,
		RunID:         resp.Data.Run.RunID,
		WebThreadLink: resp.Data.WebThreadLink,
	}}
	// The recovery response carries the preallocated Project, Thread and Run.
	if resp.Data.Run.State == 0 && resp.Data.Thread.ThreadID == result.ThreadID && resp.Data.Thread.ProjectID != "" {
		result.ProjectID = resp.Data.Thread.ProjectID.String()
		result.SubmissionStatus = "unknown"
		result.Warning = "提交结果暂不明确。请使用返回的 thread_id 和 run_id 查询原任务，勿重新提交。"
	}
	return result, nil
}
