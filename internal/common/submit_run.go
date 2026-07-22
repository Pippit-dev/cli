package common

import (
	"context"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

// AgentNameVideoPart is the video-part agent used by direct video capabilities.
const AgentNameVideoPart = "pippit_video_part_agent"

// SubmitRunResult is the shared successful submit_run output.
type SubmitRunResult struct {
	ThreadID      string `json:"thread_id"`
	RunID         string `json:"run_id"`
	WebThreadLink string `json:"web_thread_link"`
}

// SubmitRunResponse is the shared response envelope returned by submit_run.
type SubmitRunResponse struct {
	Ret    string                `json:"ret"`
	Errmsg string                `json:"errmsg"`
	LogID  string                `json:"log_id"`
	Data   SubmitRunResponseData `json:"data"`
}

type SubmitRunResponseData struct {
	WebThreadLink string               `json:"web_thread_link"`
	Run           SubmitRunResponseRun `json:"run"`
}

type SubmitRunResponseRun struct {
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id"`
}

// SubmitRun sends a submit_run request and validates its shared response envelope.
func SubmitRun(ctx context.Context, command string, body any, runner *Runner) (*SubmitRunResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("%s 运行器客户端缺失", command)
	}
	var resp SubmitRunResponse
	if err := runner.Client.SendRequest(ctx, SubmitRunPath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("提交 %s 请求失败: %w", command, err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, NewLogIDError(fmt.Sprintf("%s 请求返回失败: ret=%s errmsg=%s", command, resp.Ret, resp.Errmsg), resp.LogID)
	}
	if resp.Data.Run.ThreadID == "" {
		return nil, fmt.Errorf("%s 响应缺少 data.run.thread_id", command)
	}
	if resp.Data.Run.RunID == "" {
		return nil, fmt.Errorf("%s 响应缺少 data.run.run_id", command)
	}

	return &SubmitRunResult{
		ThreadID:      resp.Data.Run.ThreadID,
		RunID:         resp.Data.Run.RunID,
		WebThreadLink: resp.Data.WebThreadLink,
	}, nil
}

// SubmitRunPath returns the configured submit_run endpoint path.
func SubmitRunPath(runner *Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.SubmitRun != "" {
		return runner.Config.Paths.SubmitRun
	}
	return config.SubmitRunPath
}
