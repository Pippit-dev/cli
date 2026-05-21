package short_drama

import (
	"context"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type submitRunResponse struct {
	Ret    string `json:"ret"`
	Errmsg string `json:"errmsg"`
	Data   struct {
		WebThreadLink string `json:"web_thread_link"`
		Run           struct {
			ThreadID string `json:"thread_id"`
			RunID    string `json:"run_id"`
		} `json:"run"`
	} `json:"data"`
}

func SubmitRun(ctx context.Context, opts *SubmitRunOptions, runner *common.Runner) (*SubmitRunResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("submit_run runner client is required")
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
	if opts.AgentName != "" {
		body["agent_name"] = opts.AgentName
	}

	var resp submitRunResponse
	if err := runner.Client.SendRequest(ctx, submitRunPath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("submit_run request failed: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "unknown error"
		}
		return nil, fmt.Errorf("submit_run failed: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
	}
	if resp.Data.Run.ThreadID == "" {
		return nil, fmt.Errorf("submit_run response missing data.run.thread_id")
	}
	if resp.Data.Run.RunID == "" {
		return nil, fmt.Errorf("submit_run response missing data.run.run_id")
	}
	return &SubmitRunResult{
		ThreadID:      resp.Data.Run.ThreadID,
		RunID:         resp.Data.Run.RunID,
		WebThreadLink: resp.Data.WebThreadLink,
	}, nil
}

func submitRunPath(runner *common.Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.SubmitRun != "" {
		return runner.Config.Paths.SubmitRun
	}
	return config.SubmitRunPath
}
