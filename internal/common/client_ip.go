package common

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type ClientIPResult struct {
	ClientIP string `json:"client_ip"`
}

type clientIPRequestPolicy struct {
	adapter.HTTPAdapter
}

func (clientIPRequestPolicy) PrepareAPIRequest(context.Context, *http.Request) (adapter.AuthDecision, error) {
	return adapter.UseDefaultAuth, nil
}

// GetClientIP keeps the host transport and routing, without resolving account credentials.
func GetClientIP(ctx context.Context, runner *Runner, policy adapter.HTTPAdapter) (*ClientIPResult, error) {
	if runner == nil || runner.Config == nil {
		return nil, fmt.Errorf("查询出口 IP 的运行配置缺失")
	}
	cfg := runner.Config
	anonymousPolicy := clientIPRequestPolicy{HTTPAdapter: adapter.HTTPOrDefault(policy)}
	client := NewHTTPClientWithAdapter(cfg.BaseURL, cfg.HTTPTimeout, NewAccessKeyAuthorizer(""), adapter.WithPPE(anonymousPolicy, cfg.PPEEnv))
	// This query is anonymous even when the caller's context carries a selected team.
	ctx = WithSpace(ctx, nil)
	var resp struct {
		Ret    string          `json:"ret"`
		Errmsg string          `json:"errmsg"`
		LogID  string          `json:"log_id"`
		Data   *ClientIPResult `json:"data"`
	}
	if err := client.SendRequest(ctx, config.GetClientIPPath, nil, &resp); err != nil {
		return nil, fmt.Errorf("查询出口 IP 请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, NewLogIDError(fmt.Sprintf("查询出口 IP 请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg), resp.LogID)
	}
	if resp.Data == nil || resp.Data.ClientIP == "" {
		return nil, NewLogIDError("查询出口 IP 响应缺少 data.client_ip", resp.LogID)
	}
	ip, err := netip.ParseAddr(resp.Data.ClientIP)
	if err != nil || ip.Zone() != "" {
		return nil, NewLogIDError("查询出口 IP 响应的 data.client_ip 无效", resp.LogID)
	}
	return &ClientIPResult{ClientIP: ip.Unmap().String()}, nil
}
