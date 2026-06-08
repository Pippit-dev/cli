package common

import (
	"context"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

const getThreadVersionV2 = "v2"

// GetThreadOptions is the stable command-facing request shape for thread lookup.
type GetThreadOptions struct {
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id,omitempty"`
}

// GetThreadResult is the parsed get_thread response used by `pippit-tool-cli short-drama +get-thread`.
type GetThreadResult struct {
	ReadableText string `json:"readable_text"`
}

type getThreadResponse struct {
	Ret    string `json:"ret"`
	Errmsg string `json:"errmsg"`
	Data   struct {
		ReadableText string `json:"readable_text"`
	} `json:"data"`
}

func GetThread(ctx context.Context, opts *GetThreadOptions, runner *Runner) (*GetThreadResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("get_thread 运行器客户端缺失")
	}

	body := map[string]any{
		"thread_id": opts.ThreadID,
		"version":   getThreadVersionV2,
	}
	if opts.RunID != "" {
		body["run_id"] = opts.RunID
	}

	var resp getThreadResponse
	if err := runner.Client.SendRequest(ctx, getThreadPath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("获取线程请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, fmt.Errorf("获取线程请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
	}
	if resp.Data.ReadableText == "" {
		return nil, fmt.Errorf("get_thread 响应缺少 data.readable_text")
	}

	return &GetThreadResult{
		ReadableText: resp.Data.ReadableText,
	}, nil
}

func getThreadPath(runner *Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.GetThread != "" {
		return runner.Config.Paths.GetThread
	}
	return config.GetThreadPath
}
