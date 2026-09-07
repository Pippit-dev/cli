package common

import (
	"context"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

// GetCreditBalanceResult is the JSON output printed by `pippit-tool-cli get-credit-balance`.
type GetCreditBalanceResult struct {
	TotalRemainAmount int64  `json:"total_remain_amount,string"`
	LogID             string `json:"-"`
}

type getCreditBalanceResponse struct {
	Ret    string                        `json:"ret"`
	Errmsg string                        `json:"errmsg"`
	LogID  string                        `json:"log_id"`
	Data   *getCreditBalanceResponseData `json:"data"`
}

type getCreditBalanceResponseData struct {
	TotalRemainAmount *int64 `json:"total_remain_amount,string"`
}

// GetCreditBalance queries the effective credit balance of the access-key owner.
func GetCreditBalance(ctx context.Context, runner *Runner) (*GetCreditBalanceResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("get_credit_balance 运行器客户端缺失")
	}

	var resp getCreditBalanceResponse
	if err := runner.Client.SendRequest(ctx, getCreditBalancePath(runner), struct{}{}, &resp); err != nil {
		return nil, fmt.Errorf("获取积分余额请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, NewLogIDError(fmt.Sprintf("获取积分余额请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg), resp.LogID)
	}
	if resp.Data == nil {
		return nil, NewLogIDError("get_credit_balance 响应缺少 data", resp.LogID)
	}
	if resp.Data.TotalRemainAmount == nil {
		return nil, NewLogIDError("get_credit_balance 响应缺少 data.total_remain_amount", resp.LogID)
	}

	return &GetCreditBalanceResult{
		TotalRemainAmount: *resp.Data.TotalRemainAmount,
		LogID:             resp.LogID,
	}, nil
}

func getCreditBalancePath(runner *Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.GetCreditBalance != "" {
		return runner.Config.Paths.GetCreditBalance
	}
	return config.GetCreditBalancePath
}
