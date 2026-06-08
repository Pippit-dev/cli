package common

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

const GetThreadVersionV2 = "v2"

// GetThreadOptions is the stable command-facing request shape for thread lookup.
type GetThreadOptions struct {
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id,omitempty"`
	Version  string `json:"version,omitempty"`
}

// GetThreadResult is the parsed get_thread response used by `pippit-tool-cli get-thread`.
type GetThreadResult struct {
	ReadableText string `json:"readable_text"`
	RawData      []byte `json:"-"`
}

type getThreadResponse struct {
	Ret    string          `json:"ret"`
	Errmsg string          `json:"errmsg"`
	LogID  string          `json:"log_id"`
	Data   json.RawMessage `json:"data"`
}

func GetThread(ctx context.Context, opts *GetThreadOptions, runner *Runner) (*GetThreadResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("get_thread 运行器客户端缺失")
	}

	body := map[string]any{
		"thread_id": opts.ThreadID,
	}
	if opts.RunID != "" {
		body["run_id"] = opts.RunID
	}
	if opts.Version != "" {
		body["version"] = opts.Version
	}

	var resp getThreadResponse
	if err := runner.Client.SendRequest(ctx, getThreadPath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("获取线程请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, NewLogIDError(fmt.Sprintf("获取线程请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg), resp.LogID)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("get_thread 响应缺少 data")
	}
	readable := struct {
		ReadableText string `json:"readable_text"`
	}{}
	if opts.Version == GetThreadVersionV2 {
		_ = json.Unmarshal(resp.Data, &readable)
		if readable.ReadableText == "" {
			return nil, fmt.Errorf("get_thread v2 响应缺少 data.readable_text")
		}
	}

	return &GetThreadResult{
		ReadableText: readable.ReadableText,
		RawData:      resp.Data,
	}, nil
}

func getThreadPath(runner *Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.GetThread != "" {
		return runner.Config.Paths.GetThread
	}
	return config.GetThreadPath
}
