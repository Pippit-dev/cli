package common

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type GetRunCreditsOptions struct {
	RunID    string `json:"run_id"`
	ThreadID string `json:"thread_id,omitempty"`
}

type GetRunCreditsResult struct {
	TeamID   string     `json:"team_id"`
	ThreadID string     `json:"thread_id"`
	RunID    string     `json:"run_id"`
	Credits  RunCredits `json:"credits"`
}

type RunCredits struct {
	Deducted         *int64 `json:"deducted"`
	PreDeducted      *int64 `json:"preDeducted,omitempty"`
	Returned         *int64 `json:"returned,omitempty"`
	SettlementStatus *int64 `json:"settlementStatus,omitempty"`
}

type runCreditSnapshot struct {
	ActualDeductCredits json.RawMessage `json:"actual_deduct_credits"`
	PreDeductCredits    json.RawMessage `json:"pre_deduct_credits"`
	ReturnedCredits     json.RawMessage `json:"returned_credits"`
	Status              json.RawMessage `json:"status"`
	UpdatedAtMS         json.RawMessage `json:"updated_at_ms"`
}

func GetRunCredits(ctx context.Context, opts GetRunCreditsOptions, runner *Runner) (*GetRunCreditsResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("get_run_credits 运行器客户端缺失")
	}
	var resp struct {
		Ret    string `json:"ret"`
		Errmsg string `json:"errmsg"`
		LogID  string `json:"log_id"`
		Data   struct {
			Run struct {
				RunID       string            `json:"run_id"`
				ThreadID    string            `json:"thread_id"`
				State       int               `json:"state"`
				Consumption runCreditSnapshot `json:"credit_consumption"`
			} `json:"run"`
		} `json:"data"`
	}
	if err := runner.Client.SendRequest(ctx, config.GetRunCreditsPath, opts, &resp); err != nil {
		return nil, fmt.Errorf("查询 Run 积分消耗失败: %w", err)
	}
	if resp.Ret != "0" {
		return nil, NewLogIDError(fmt.Sprintf("查询 Run 积分消耗失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg), resp.LogID)
	}

	run := resp.Data.Run
	if run.RunID != opts.RunID || run.ThreadID == "" || (opts.ThreadID != "" && opts.ThreadID != run.ThreadID) {
		return nil, NewLogIDError("积分响应的 Run 或 Thread 与请求不匹配", resp.LogID)
	}
	return &GetRunCreditsResult{
		TeamID: SpaceTeamID(ctx), ThreadID: run.ThreadID, RunID: run.RunID,
		Credits: run.Consumption.credits(run.State),
	}, nil
}

func (snapshot runCreditSnapshot) credits(state int) RunCredits {
	credits := RunCredits{
		PreDeducted:      creditInteger(snapshot.PreDeductCredits),
		Returned:         creditInteger(snapshot.ReturnedCredits),
		SettlementStatus: creditInteger(snapshot.Status),
	}
	actual := creditInteger(snapshot.ActualDeductCredits)
	if actual == nil || credits.SettlementStatus == nil {
		return credits
	}
	// RunState: Completed, Failed, Canceled, Interrupted.
	switch state {
	case 3, 4, 5, 9:
	default:
		return credits
	}
	// RunCreditConsumptionStatus: NoConsumption, Deducted, PartialReturned, Returned.
	switch *credits.SettlementStatus {
	case 4, 5, 6:
		credits.Deducted = actual
	case 1:
		// A default zero without a ledger row does not confirm free usage.
		if updated := creditInteger(snapshot.UpdatedAtMS); *actual == 0 && updated != nil && *updated > 0 {
			credits.Deducted = actual
		}
	}
	return credits
}

func creditInteger(raw json.RawMessage) *int64 {
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return nil
	}
	return &n
}
