package common

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DownloadClient is a minimal interface for downloading files via HTTP GET.
// It is satisfied by common.Client so that download logic can use the same
// HTTP infrastructure (headers, auth, timeouts) as API calls.

// DownloadResultOptions is the command-facing shape for downloading one result URL.
type DownloadResultOptions struct {
	URL        string `json:"url"`
	OutputPath string `json:"output_path"`
	UpdatedAt  int64  `json:"updated_at,omitempty"`
	Workers    int    `json:"workers"`
}

type DownloadResultError struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// DownloadResultResponse is the JSON envelope printed by `pippit-tool-cli short-drama +download-result`.
type DownloadResultResponse struct {
	OutputPath   string                 `json:"output_path"`
	Downloaded   []string               `json:"downloaded"`
	AlreadyExist []string               `json:"already_exist,omitempty"`
	Errors       []*DownloadResultError `json:"errors,omitempty"`
}

const (
	maxDownloadRetries = 3
	retryBaseDelay     = 500 * time.Millisecond
)

type downloadTask struct {
	url       string
	filepath  string
	updatedAt int64
}

type downloadTaskResult struct {
	filepath string
	err      error
}

func DownloadResult(ctx context.Context, opts DownloadResultOptions, runner *Runner) (*DownloadResultResponse, error) {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("下载已取消")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("下载超时")
		}
		return nil, fmt.Errorf("下载上下文异常: %w", err)
	}
	rawURL := strings.TrimSpace(opts.URL)
	if rawURL == "" {
		return nil, fmt.Errorf("下载 URL 不能为空")
	}

	outputPath := strings.TrimSpace(opts.OutputPath)
	if outputPath == "" {
		return nil, fmt.Errorf("输出路径不能为空")
	}
	if info, err := os.Stat(outputPath); err == nil {
		if info.IsDir() {
			return nil, fmt.Errorf("输出路径 %q 是目录，请指定文件", outputPath)
		}
		if shouldSkipExistingFile(info, opts.UpdatedAt) {
			return &DownloadResultResponse{
				OutputPath:   outputPath,
				AlreadyExist: []string{outputPath},
			}, nil
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("获取输出路径信息失败: %w", err)
	}
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %w", err)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("URL %q 不合法: %w", rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("URL %q 的协议 %q 不支持，仅支持 http 和 https", rawURL, parsed.Scheme)
	}
	tasks := []downloadTask{
		{
			url:       rawURL,
			filepath:  outputPath,
			updatedAt: opts.UpdatedAt,
		},
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = 5
	}
	if workers > len(tasks) {
		workers = len(tasks)
	}

	taskCh := make(chan downloadTask)
	resultCh := make(chan downloadTaskResult, len(tasks))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				resultCh <- downloadTaskResult{
					filepath: task.filepath,
					err:      downloadFileWithRetry(ctx, runner.Client, task.url, task.filepath, task.updatedAt),
				}
			}
		}()
	}

	go func() {
		defer close(taskCh)
		for _, task := range tasks {
			select {
			case <-ctx.Done():
				return
			case taskCh <- task:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	downloaded := make([]string, 0, len(tasks))
	errorList := make([]*DownloadResultError, 0)
	for result := range resultCh {
		if result.err != nil {
			errorList = append(errorList, &DownloadResultError{
				File:  result.filepath,
				Error: result.err.Error(),
			})
			continue
		}
		downloaded = append(downloaded, result.filepath)
	}
	sort.Strings(downloaded)
	sort.Slice(errorList, func(i, j int) bool {
		return errorList[i].File < errorList[j].File
	})

	result := &DownloadResultResponse{
		OutputPath: outputPath,
		Downloaded: downloaded,
		Errors:     errorList,
	}
	if len(downloaded) == 0 && len(errorList) > 0 {
		return result, fmt.Errorf("全部 %d 个下载任务失败", len(errorList))
	}
	return result, nil
}

func shouldSkipExistingFile(info os.FileInfo, updatedAt int64) bool {
	if updatedAt <= 0 {
		return true
	}
	return info.ModTime().Unix() >= updatedAt
}

func downloadFileWithRetry(ctx context.Context, client Client, rawURL string, targetPath string, updatedAt int64) error {
	var lastErr error
	for attempt := 0; attempt <= maxDownloadRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay * time.Duration(1<<(attempt-1))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		err := downloadFile(ctx, client, rawURL, targetPath, updatedAt)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryableError(err) {
			return err
		}
	}
	return fmt.Errorf("重试 %d 次后仍失败: %w", maxDownloadRetries, lastErr)
}

func isRetryableError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Timeout() || urlErr.Temporary()
	}
	return false
}

func downloadFile(ctx context.Context, client Client, rawURL string, targetPath string, updatedAt int64) error {
	var resp *http.Response
	if err := client.SendRequest(ctx, rawURL, nil, &resp); err != nil {
		return err
	}
	defer resp.Body.Close()

	tmpPath := targetPath + ".tmp"
	tempActive := true
	defer func() {
		if tempActive {
			_ = os.Remove(tmpPath)
		}
	}()
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("写入临时文件失败: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("关闭临时文件失败: %w", closeErr)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("替换目标文件失败: %w", err)
	}
	tempActive = false
	if updatedAt > 0 {
		modTime := time.Unix(updatedAt, 0)
		_ = os.Chtimes(targetPath, time.Now(), modTime)
	}
	return nil
}
