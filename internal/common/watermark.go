package common

import (
	"context"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type WatermarkResult struct {
	Enabled bool `json:"enabled"`
}

type watermarkResponse struct {
	Ret    string `json:"ret"`
	Errmsg string `json:"errmsg"`
	LogID  string `json:"log_id"`
	Data   *struct {
		Enabled *bool `json:"enabled"`
	} `json:"data"`
}

// Watermark queries when enabled is nil; updates return the server's readback in the same request.
func Watermark(ctx context.Context, runner *Runner, enabled *bool) (*WatermarkResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("水印设置运行器客户端缺失")
	}
	path := config.GetWatermarkSettingPath
	var body any = struct{}{}
	if enabled != nil {
		path = config.UpdateWatermarkSettingPath
		body = struct {
			Enabled bool `json:"enabled"`
		}{Enabled: *enabled}
	}

	var resp watermarkResponse
	if err := runner.Client.SendRequest(ctx, path, body, &resp); err != nil {
		if enabled != nil {
			return nil, fmt.Errorf("设置水印请求失败，结果可能已生效，请运行 watermark 查询确认: %w", err)
		}
		return nil, fmt.Errorf("查询水印设置请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, NewLogIDError(fmt.Sprintf("水印设置请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg), resp.LogID)
	}
	if resp.Data == nil || resp.Data.Enabled == nil {
		return nil, NewLogIDError("水印设置响应缺少 data.enabled；请查询确认当前状态", resp.LogID)
	}
	return &WatermarkResult{Enabled: *resp.Data.Enabled}, nil
}
