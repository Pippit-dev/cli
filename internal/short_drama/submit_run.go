package short_drama

import (
	"context"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

// SubmitRunOptions is the stable command-facing request shape for short drama run submission.
type SubmitRunOptions struct {
	Message  string   `json:"message"`
	ThreadID string   `json:"thread_id,omitempty"`
	AssetIDs []string `json:"asset_ids,omitempty"`
}

// SubmitRunResult is the JSON envelope printed by `pippit-tool-cli short-drama +submit-run`.
type SubmitRunResult struct {
	ThreadID      string `json:"thread_id"`
	RunID         string `json:"run_id"`
	WebThreadLink string `json:"web_thread_link"`
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

	var resp common.SubmitRunResponse
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
	return &SubmitRunResult{
		ThreadID:      resp.Data.Run.ThreadID,
		RunID:         resp.Data.Run.RunID,
		WebThreadLink: resp.Data.WebThreadLink,
	}, nil
}
