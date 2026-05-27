package short_drama

import (
	"context"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

// GetThreadOptions is the stable command-facing request shape for thread lookup.
type GetThreadOptions struct {
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id,omitempty"`
	AfterSeq int    `json:"after_seq"`
}

// ThreadEntry is a compact message or artifact entry inside a thread run.
type ThreadEntry struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

// GetThreadResult is the JSON envelope printed by `pippit-tool-cli short-drama +get-thread`.
type GetThreadResult struct {
	Messages []*ThreadEntry `json:"messages"`
}

type getThreadResponse struct {
	Ret    string `json:"ret"`
	Errmsg string `json:"errmsg"`
	Data   struct {
		Thread struct {
			RunList []getThreadRun `json:"run_list"`
		} `json:"thread"`
	} `json:"data"`
}

type getThreadRun struct {
	State      int              `json:"state"`
	FailReason string           `json:"fail_reason"`
	EntryList  []getThreadEntry `json:"entry_list"`
}

type getThreadEntry struct {
	Message  *getThreadMessage  `json:"message"`
	Artifact *getThreadArtifact `json:"artifact"`
}

type getThreadMessage struct {
	MessageID       string `json:"message_id"`
	Role            string `json:"role"`
	Content         []any  `json:"content"`
	ClientToolCalls []any  `json:"client_tool_calls"`
}

type getThreadArtifact struct {
	ArtifactID string `json:"artifact_id"`
	Role       string `json:"role"`
	Content    []any  `json:"content"`
}

func GetThread(ctx context.Context, opts *GetThreadOptions, runner *common.Runner) (*GetThreadResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("get_thread runner client is required")
	}

	body := map[string]any{
		"thread_id": opts.ThreadID,
		"after_seq": opts.AfterSeq,
	}
	if opts.RunID != "" {
		body["run_id"] = opts.RunID
	}

	var resp getThreadResponse
	if err := runner.Client.SendRequest(ctx, getThreadPath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("get_thread request failed: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "unknown error"
		}
		return nil, fmt.Errorf("get_thread failed: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
	}
	if len(resp.Data.Thread.RunList) == 0 {
		return nil, fmt.Errorf("get_thread response missing data.thread.run_list")
	}

	run := resp.Data.Thread.RunList[0]
	// 4: failed
	// 5: canceled
	if run.State == 4 {
		if run.FailReason == "" {
			run.FailReason = "unknown failure"
		}
		return nil, fmt.Errorf("get_thread run failed: %s", run.FailReason)
	}
	if run.State == 5 {
		return nil, fmt.Errorf("get_thread run canceled")
	}

	return &GetThreadResult{
		Messages: extractThreadEntries(run),
	}, nil
}

func extractThreadEntries(run getThreadRun) []*ThreadEntry {
	entries := make([]*ThreadEntry, 0, len(run.EntryList))
	for _, entry := range run.EntryList {
		if entry.Message != nil {
			content := append([]any(nil), entry.Message.Content...)
			content = append(content, entry.Message.ClientToolCalls...)
			entries = append(entries, &ThreadEntry{
				ID:      entry.Message.MessageID,
				Role:    entry.Message.Role,
				Content: content,
			})
		}
		if entry.Artifact != nil {
			entries = append(entries, &ThreadEntry{
				ID:      entry.Artifact.ArtifactID,
				Role:    entry.Artifact.Role,
				Content: entry.Artifact.Content,
			})
		}
	}
	return entries
}

func getThreadPath(runner *common.Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.GetThread != "" {
		return runner.Config.Paths.GetThread
	}
	return config.GetThreadPath
}
