package generate_video

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const (
	successRunState = 3
	failedRunState  = 4
)

// QueryResultOptions is the command-facing request shape for query-result.
type QueryResultOptions struct {
	ThreadID    string
	RunID       string
	DownloadDir string
}

// QueryResultResult describes the user-facing query-result outcome.
type QueryResultResult struct {
	Completed    bool               `json:"completed"`
	ThreadID     string             `json:"thread_id"`
	RunID        string             `json:"run_id"`
	ErrorMessage string             `json:"error_message"`
	Videos       []QueryResultVideo `json:"videos"`
	Images       []QueryResultImage `json:"images"`
}

// QueryResultVideo describes a downloaded video from query-result.
type QueryResultVideo struct {
	DownloadURL string `json:"download_url"`
	OutputPath  string `json:"output_path"`
}

// QueryResultImage describes a downloaded image from query-result.
type QueryResultImage struct {
	DownloadURL string `json:"download_url"`
	OutputPath  string `json:"output_path"`
}

type queryThread struct {
	ThreadID string     `json:"thread_id"`
	RunList  []queryRun `json:"run_list"`
}

type queryRun struct {
	RunID        string       `json:"run_id"`
	State        int          `json:"state"`
	ErrorMessage string       `json:"error_message"`
	ErrorMsg     string       `json:"error_msg"`
	Errmsg       string       `json:"errmsg"`
	EntryList    []queryEntry `json:"entry_list"`
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
	Video        *queryVideo     `json:"video"`
	Image        *queryImage     `json:"image"`
	ErrorMessage string          `json:"error_message"`
	ErrorCode    json.RawMessage `json:"error_code"`
}

type queryVideo struct {
	DownloadURL string `json:"download_url"`
	Title       string `json:"title"`
	VID         string `json:"vid"`
	AssetID     string `json:"asset_id"`
}

type queryImage struct {
	DownloadURL string         `json:"url"`
	AssetID     string         `json:"asset_id"`
	Metadata    queryImageMeta `json:"metadata"`
}

type queryImageMeta struct {
	Format string `json:"format"`
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
		if result, ok := queryResultFromGetThreadBusinessError(err, opts); ok {
			return result, nil
		}
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
	if run.State != successRunState {
		result := &QueryResultResult{
			Completed: run.State == failedRunState,
			ThreadID:  firstNonEmpty(thread.ThreadID, opts.ThreadID),
			RunID:     opts.RunID,
			Videos:    []QueryResultVideo{},
			Images:    []QueryResultImage{},
		}
		if run.State == failedRunState {
			result.ErrorMessage = firstNonEmpty(extractQueryErrorMessage(run), "Run 失败")
		}
		return result, nil
	}

	videos := extractQueryVideos(run)
	images := extractQueryImages(run)
	if len(videos) == 0 && len(images) == 0 {
		return nil, fmt.Errorf("下载失败：未找到可下载的产物")
	}

	downloadDir, err := common.ExpandPath(opts.DownloadDir)
	if err != nil {
		return nil, fmt.Errorf("下载失败：解析下载目录失败：%w", err)
	}

	usedNames := make(map[string]int, len(videos)+len(images))

	resultVideos := make([]QueryResultVideo, 0, len(videos))
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
		actualOutputPath := outputPath
		if len(download.Downloaded) > 0 {
			actualOutputPath = download.Downloaded[0]
		} else if len(download.AlreadyExist) > 0 {
			actualOutputPath = download.AlreadyExist[0]
		}
		resultVideos = append(resultVideos, QueryResultVideo{
			DownloadURL: video.DownloadURL,
			OutputPath:  actualOutputPath,
		})
	}

	resultImages := make([]QueryResultImage, 0, len(images))
	for i, image := range images {
		if strings.TrimSpace(image.DownloadURL) == "" {
			return nil, fmt.Errorf("下载失败：第 %d 个图片产物 download_url 为空", i+1)
		}
		outputPath := filepath.Join(downloadDir, uniqueQueryResultFileName(imageFileName(image, i+1), usedNames))
		download, err := common.DownloadResult(ctx, common.DownloadResultOptions{
			URL:        image.DownloadURL,
			OutputPath: outputPath,
			Workers:    5,
		}, runner)
		if err != nil {
			return nil, fmt.Errorf("下载失败：%w", err)
		}
		actualOutputPath := outputPath
		if len(download.Downloaded) > 0 {
			actualOutputPath = download.Downloaded[0]
		} else if len(download.AlreadyExist) > 0 {
			actualOutputPath = download.AlreadyExist[0]
		}
		resultImages = append(resultImages, QueryResultImage{
			DownloadURL: image.DownloadURL,
			OutputPath:  actualOutputPath,
		})
	}

	return &QueryResultResult{
		Completed: true,
		ThreadID:  firstNonEmpty(thread.ThreadID, opts.ThreadID),
		RunID:     opts.RunID,
		Videos:    resultVideos,
		Images:    resultImages,
	}, nil
}

func queryResultFromGetThreadBusinessError(err error, opts *QueryResultOptions) (*QueryResultResult, bool) {
	var logErr *common.LogIDError
	if !errors.As(err, &logErr) {
		return nil, false
	}
	message := getThreadBusinessErrorMessage(logErr.Message)
	if message == "" {
		message = "查询失败"
	}
	if logID := logErr.LogID(); logID != "" {
		message = fmt.Sprintf("%s log_id=%s", message, logID)
	}
	return &QueryResultResult{
		Completed:    true,
		ThreadID:     opts.ThreadID,
		RunID:        opts.RunID,
		ErrorMessage: message,
		Videos:       []QueryResultVideo{},
		Images:       []QueryResultImage{},
	}, true
}

func getThreadBusinessErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if idx := strings.Index(message, "errmsg="); idx >= 0 {
		return strings.TrimSpace(message[idx+len("errmsg="):])
	}
	return message
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

func extractQueryImages(run queryRun) []queryImage {
	images := make([]queryImage, 0)
	for _, entry := range run.EntryList {
		artifact := entry.Artifact
		for _, content := range artifact.Content {
			if content.SubType != "biz/x_data_image" {
				continue
			}
			data := content.Data
			if data.Image != nil {
				images = append(images, *data.Image)
			}
		}
	}
	return images
}

func extractQueryErrorMessage(run queryRun) string {
	if message := firstNonEmpty(run.ErrorMessage, run.ErrorMsg, run.Errmsg); message != "" {
		return message
	}
	for _, entry := range run.EntryList {
		for _, content := range entry.Artifact.Content {
			data := content.Data
			if message := firstNonEmpty(data.ErrorMessage); message != "" {
				if code := rawMessageString(data.ErrorCode); code != "" {
					return fmt.Sprintf("%s (error_code=%s)", message, code)
				}
				return message
			}
			if code := rawMessageString(data.ErrorCode); code != "" {
				return "error_code=" + code
			}
		}
	}
	return ""
}

func rawMessageString(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(raw))
}

func videoFileName(video queryVideo, index int) string {
	name := firstNonEmpty(video.VID, video.Title, video.AssetID)
	if name == "" {
		name = "result_" + strconv.Itoa(index)
	}
	name = sanitizeFileName(name)
	if !hasVideoExtension(name) {
		name += ".mp4"
	}
	return name
}

func hasVideoExtension(name string) bool {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(name))) {
	case ".mp4", ".mov", ".m4v", ".webm":
		return true
	default:
		return false
	}
}

func imageFileName(image queryImage, index int) string {
	name := image.AssetID
	if name == "" {
		name = "result_" + strconv.Itoa(index)
	}
	name = sanitizeFileName(name)
	if !hasImageExtension(name) {
		name += "." + normalizeImageFormatExt(image.Metadata.Format)
	}
	return name
}

// normalizeImageFormatExt maps the server-provided metadata.format to a safe
// file extension. Only a known allowlist is accepted; anything else (including
// "image/jpeg", ".jpeg", or empty values) falls back to "png".
func normalizeImageFormatExt(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	format = strings.TrimPrefix(format, "image/")
	format = strings.TrimPrefix(format, ".")
	switch format {
	case "jpg", "jpeg", "png", "gif", "bmp", "webp", "svg":
		return format
	default:
		return "png"
	}
}

func hasImageExtension(name string) bool {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(name))) {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg":
		return true
	default:
		return false
	}
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
		return "result"
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
