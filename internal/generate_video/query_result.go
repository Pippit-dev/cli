package generate_video

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const completedRunState = 3

// QueryResultOptions is the command-facing request shape for query-result.
type QueryResultOptions struct {
	ThreadID    string
	RunID       string
	DownloadDir string
}

// QueryResultResult describes the user-facing query-result outcome.
type QueryResultResult struct {
	Completed   bool
	State       int
	OutputPaths []string
}

type queryThread struct {
	ThreadID string     `json:"thread_id"`
	RunList  []queryRun `json:"run_list"`
}

type queryRun struct {
	RunID     string       `json:"run_id"`
	State     int          `json:"state"`
	EntryList []queryEntry `json:"entry_list"`
}

type queryEntry struct {
	Artifact queryArtifact `json:"artifact"`
}

type queryArtifact struct {
	Content []queryContent `json:"content"`
}

type queryContent struct {
	SubType string           `json:"sub_type"`
	Data    queryContentData `json:"data"`
}

type queryContentData struct {
	Video *queryVideo `json:"video"`
}

type queryVideo struct {
	DownloadURL string `json:"download_url"`
	Title       string `json:"title"`
	VID         string `json:"vid"`
	AssetID     string `json:"asset_id"`
}

func QueryResult(ctx context.Context, opts *QueryResultOptions, runner *common.Runner) (*QueryResultResult, error) {
	if err := validateQueryResultOptions(opts); err != nil {
		return nil, err
	}

	threadResult, err := common.GetThread(ctx, &common.GetThreadOptions{
		ThreadID: opts.ThreadID,
		RunID:    opts.RunID,
	}, runner)
	if err != nil {
		return nil, fmt.Errorf("查询失败：%w", err)
	}

	thread, err := parseQueryThread(threadResult)
	if err != nil {
		return nil, fmt.Errorf("查询失败：%w", err)
	}

	run, ok := findQueryRun(thread, opts.RunID)
	if !ok {
		return nil, fmt.Errorf("查询失败：未找到 run_id=%s 对应的 Run", opts.RunID)
	}
	if run.State != completedRunState {
		return &QueryResultResult{
			Completed: false,
			State:     run.State,
		}, nil
	}

	videos := extractQueryVideos(run)
	if len(videos) == 0 {
		return nil, fmt.Errorf("下载失败：未找到可下载的视频产物")
	}

	downloadDir, err := expandPath(opts.DownloadDir)
	if err != nil {
		return nil, fmt.Errorf("下载失败：解析下载目录失败：%w", err)
	}

	outputPaths := make([]string, 0, len(videos))
	usedNames := make(map[string]int, len(videos))
	for i, video := range videos {
		if strings.TrimSpace(video.DownloadURL) == "" {
			return nil, fmt.Errorf("下载失败：第 %d 个视频产物 download_url 为空", i+1)
		}
		outputPath := filepath.Join(downloadDir, uniqueQueryResultFileName(videoFileName(video, i+1), usedNames))
		download, err := common.DownloadResult(ctx, common.DownloadResultOptions{
			URL:        video.DownloadURL,
			OutputPath: outputPath,
			Workers:    5,
		}, runner)
		if err != nil {
			return nil, fmt.Errorf("下载失败：%w", err)
		}
		if len(download.Downloaded) > 0 {
			outputPaths = append(outputPaths, download.Downloaded...)
			continue
		}
		if len(download.AlreadyExist) > 0 {
			outputPaths = append(outputPaths, download.AlreadyExist...)
			continue
		}
		outputPaths = append(outputPaths, outputPath)
	}

	return &QueryResultResult{
		Completed:   true,
		State:       run.State,
		OutputPaths: outputPaths,
	}, nil
}

func validateQueryResultOptions(opts *QueryResultOptions) error {
	if opts == nil {
		return fmt.Errorf("查询失败：缺少必填参数 --thread-id")
	}
	opts.ThreadID = strings.TrimSpace(opts.ThreadID)
	opts.RunID = strings.TrimSpace(opts.RunID)
	opts.DownloadDir = strings.TrimSpace(opts.DownloadDir)
	if opts.ThreadID == "" {
		return fmt.Errorf("查询失败：缺少必填参数 --thread-id")
	}
	if opts.RunID == "" {
		return fmt.Errorf("查询失败：缺少必填参数 --run-id")
	}
	if opts.DownloadDir == "" {
		return fmt.Errorf("查询失败：缺少必填参数 --download-dir")
	}
	return nil
}

func parseQueryThread(result *common.GetThreadResult) (*queryThread, error) {
	if result == nil {
		return nil, fmt.Errorf("get_thread 响应为空")
	}
	if len(result.RawData) > 0 {
		var data map[string]json.RawMessage
		if err := json.Unmarshal(result.RawData, &data); err == nil {
			if raw := data["thread"]; len(raw) > 0 {
				if thread, ok := decodeQueryThread(raw); ok {
					return thread, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("get_thread 响应中未找到 data.thread")
}

func decodeQueryThread(raw []byte) (*queryThread, bool) {
	var thread queryThread
	if err := json.Unmarshal(raw, &thread); err != nil {
		return nil, false
	}
	if thread.ThreadID == "" && len(thread.RunList) == 0 {
		return nil, false
	}
	return &thread, true
}

func findQueryRun(thread *queryThread, runID string) (queryRun, bool) {
	for _, run := range thread.RunList {
		if run.RunID == runID {
			return run, true
		}
	}
	return queryRun{}, false
}

func (data *queryContentData) UnmarshalJSON(raw []byte) error {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var encoded string
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return err
		}
		encoded = strings.TrimSpace(encoded)
		if encoded == "" {
			return nil
		}
		raw = []byte(encoded)
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	type alias queryContentData
	return json.Unmarshal(raw, (*alias)(data))
}

func extractQueryVideos(run queryRun) []queryVideo {
	videos := make([]queryVideo, 0)
	for _, entry := range run.EntryList {
		artifact := entry.Artifact
		for _, content := range artifact.Content {
			if content.SubType != "biz/x_data_video" {
				continue
			}
			data := content.Data
			if data.Video != nil {
				videos = append(videos, *data.Video)
			}
		}
	}
	return videos
}

func videoFileName(video queryVideo, index int) string {
	name := firstNonEmpty(video.Title, video.VID, video.AssetID)
	if name == "" {
		name = "result_" + strconv.Itoa(index)
	}
	name = sanitizeFileName(name)
	if strings.TrimSpace(filepath.Ext(name)) == "" {
		name += ".mp4"
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "result.mp4"
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
}

func uniqueQueryResultFileName(name string, used map[string]int) string {
	count := used[name] + 1
	used[name] = count
	if count == 1 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%d%s", base, count, ext)
}
