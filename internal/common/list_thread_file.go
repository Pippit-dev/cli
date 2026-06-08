package common

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

const MaxListThreadFilePageSize = 200

// ListThreadFileOptions is the command-facing request shape for listing thread files.
type ListThreadFileOptions struct {
	ThreadID string `json:"thread_id"`
	PageSize int    `json:"page_size,omitempty"`
	PageNum  int    `json:"page_num,omitempty"`
}

// ThreadFile is a file entry in a thread.
type ThreadFile struct {
	FilePath    string `json:"file_path"`
	DownloadURL string `json:"download_url"`
	UpdatedAt   *int64 `json:"updated_at,omitempty"`
}

// ListThreadFileResult is the JSON envelope printed by `pippit-tool-cli short-drama +list-thread-file`.
type ListThreadFileResult struct {
	Files   []*ThreadFile `json:"files"`
	Total   int64         `json:"total"`
	Message string        `json:"message"`
}

type listThreadFileResponse struct {
	Ret     string `json:"ret"`
	Errmsg  string `json:"errmsg"`
	SvrTime int64  `json:"svr_time"`
	LogID   string `json:"log_id"`
	Data    struct {
		Files []*threadFileResponse `json:"files"`
		Total int64                 `json:"total"`
	} `json:"data"`
}

type threadFileResponse struct {
	FilePath    string `json:"file_path"`
	DownloadURL string `json:"download_url"`
	UpdatedAt   *int64 `json:"updated_at,omitempty"`
}

func ListThreadFile(ctx context.Context, opts *ListThreadFileOptions, runner *Runner) (*ListThreadFileResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("list_thread_file 运行器客户端缺失")
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
		return nil, fmt.Errorf("获取线程文件列表请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, fmt.Errorf("获取线程文件列表请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
	}

	files := make([]*ThreadFile, 0, len(resp.Data.Files))
	for _, file := range resp.Data.Files {
		if file == nil {
			continue
		}
		files = append(files, &ThreadFile{
			FilePath:    threadFilePath(opts.ThreadID, file.FilePath),
			DownloadURL: file.DownloadURL,
			UpdatedAt:   file.UpdatedAt,
		})
	}

	return &ListThreadFileResult{
		Files:   files,
		Total:   resp.Data.Total,
		Message: listThreadFileMessage(resp.Data.Total, opts.PageNum),
	}, nil
}

func listThreadFileMessage(total int64, pageNum int) string {
	start, end := "<system-remind>", "</system-remind>"
	if pageNum <= 0 {
		pageNum = 1
	}
	var message string
	if total >= MaxListThreadFilePageSize {
		message = fmt.Sprintf("文件总数已达到 %d；请使用 --page-num %d 查询下一页", MaxListThreadFilePageSize, pageNum+1)
	} else {
		message = fmt.Sprintf("文件总数小于 %d；继续使用当前 --page-num %d 查询", MaxListThreadFilePageSize, pageNum)
	}
	return fmt.Sprintf("%s\n- %s\n%s", start, message, end)
}

func threadFilePath(threadID string, filePath string) string {
	parts := []string{strings.TrimSpace(threadID)}
	trimmedFilePath := strings.Trim(strings.TrimSpace(filePath), `/\`)
	if trimmedFilePath != "" {
		parts = append(parts, trimmedFilePath)
	}
	return "." + string(filepath.Separator) + filepath.Join(parts...)
}

func listThreadFilePath(runner *Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.ListThreadFile != "" {
		return runner.Config.Paths.ListThreadFile
	}
	return config.ListThreadFilePath
}
