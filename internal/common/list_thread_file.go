package common

import (
	"context"
	"fmt"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

// ListThreadFileOptions is the command-facing request shape for listing thread files.
type ListThreadFileOptions struct {
	ThreadID string `json:"thread_id"`
	PageSize int    `json:"page_size,omitempty"`
	PageNum  int    `json:"page_num,omitempty"`
}

// ThreadFile is a file entry in a thread.
type ThreadFile struct {
	FileName    string `json:"file_name"`
	FilePath    string `json:"file_path"`
	DownloadURL string `json:"download_url"`
}

// ListThreadFileResult is the JSON envelope printed by `pippit-cli short-drama +list-thread-file`.
type ListThreadFileResult struct {
	Files []*ThreadFile `json:"files"`
	Total int64         `json:"total"`
}

type listThreadFileResponse struct {
	Ret     string `json:"ret"`
	Errmsg  string `json:"errmsg"`
	SvrTime int64  `json:"svr_time"`
	LogID   string `json:"log_id"`
	Data    struct {
		Files []*ThreadFile `json:"files"`
		Total int64         `json:"total"`
	} `json:"data"`
}

func ListThreadFile(ctx context.Context, opts *ListThreadFileOptions, runner *Runner) (*ListThreadFileResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("list_thread_file runner client is required")
	}

	body := map[string]any{
		"thread_id": opts.ThreadID,
	}
	if opts.PageSize > 0 {
		body["page_size"] = opts.PageSize
	}
	if opts.PageNum > 0 {
		body["page_num"] = opts.PageNum
	}

	var resp listThreadFileResponse
	if err := runner.Client.SendRequest(ctx, listThreadFilePath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("list_thread_file request failed: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "unknown error"
		}
		return nil, fmt.Errorf("list_thread_file failed: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
	}

	return &ListThreadFileResult{
		Files: resp.Data.Files,
		Total: resp.Data.Total,
	}, nil
}

func listThreadFilePath(runner *Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.ListThreadFile != "" {
		return runner.Config.Paths.ListThreadFile
	}
	return config.ListThreadFilePath
}
