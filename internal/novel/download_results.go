package novel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const (
	defaultDownloadOutputDir = "./xyq_novel_output"
	maxDownloadRetries       = 3
	retryBaseDelay           = 500 * time.Millisecond
)

type downloadTask struct {
	url      string
	filepath string
}

type downloadTaskResult struct {
	filepath string
	err      error
}

func DownloadResults(ctx context.Context, opts DownloadResultsOptions, _ *common.Runner) (*DownloadResultsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(opts.URLs) == 0 {
		return nil, fmt.Errorf("download urls are required")
	}

	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		outputDir = defaultDownloadOutputDir
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	tasks := make([]downloadTask, 0, len(opts.URLs))
	for i, rawURL := range opts.URLs {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("invalid url %q: %w", rawURL, err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("invalid url scheme %q in %q, only http and https are allowed", parsed.Scheme, rawURL)
		}
		ext := downloadExtension(parsed)
		filename := fmt.Sprintf("%02d%s", i+1, ext)
		tasks = append(tasks, downloadTask{
			url:      rawURL,
			filepath: filepath.Join(outputDir, filename),
		})
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = 5
	}
	if workers > len(tasks) {
		workers = len(tasks)
	}

	client := &http.Client{Timeout: 600 * time.Second}
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
					err:      downloadFileWithRetry(ctx, client, task.url, task.filepath),
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
	errorList := make([]*DownloadResultsError, 0)
	for result := range resultCh {
		if result.err != nil {
			errorList = append(errorList, &DownloadResultsError{
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

	result := &DownloadResultsResult{
		OutputDir:  outputDir,
		Downloaded: downloaded,
		Total:      len(downloaded),
		Errors:     errorList,
	}
	if len(downloaded) == 0 && len(errorList) > 0 {
		return result, fmt.Errorf("all %d download(s) failed", len(errorList))
	}
	return result, nil
}

func downloadFileWithRetry(ctx context.Context, client *http.Client, rawURL string, targetPath string) error {
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
		err := downloadFile(ctx, client, rawURL, targetPath)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryableError(err) {
			return err
		}
	}
	return fmt.Errorf("failed after %d retries: %w", maxDownloadRetries, lastErr)
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

func downloadFile(ctx context.Context, client *http.Client, rawURL string, targetPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "Pippit-CLI/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	tmpPath := targetPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace target file: %w", err)
	}
	return nil
}

func downloadExtension(parsed *url.URL) string {
	filename := parsed.Query().Get("filename")
	if filename != "" {
		if ext := filepath.Ext(filename); ext != "" {
			return ext
		}
	}
	if ext := path.Ext(parsed.Path); ext != "" {
		return ext
	}
	return ".bin"
}
