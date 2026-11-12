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
	successRunState     = 3
	failedRunState      = 4
	canceledRunState    = 5
	interruptedRunState = 9
)

// Query-result status values. "completed" stays a terminal flag; "status"
// carries the machine-readable result so integrators need not parse text.
const (
	statusRunning       = "running"
	statusSucceeded     = "succeeded"
	statusFailed        = "failed"
	statusCanceled      = "canceled"
	statusUnknown       = "unknown"
	statusInputRequired = "input_required"
	statusInterrupted   = "interrupted"
)

// runStateStatus maps the server-side run state (0-9) to the user-facing
// status. Any unmapped value falls back to unknown instead of being guessed.
var runStateStatus = map[int]string{
	0: statusUnknown, // Unspecified
	1: statusRunning, // Submitted
	2: statusRunning, // Working
	3: statusSucceeded,
	4: statusFailed,
	5: statusCanceled,
	6: statusInputRequired,
	7: statusRunning,     // Generating
	8: statusRunning,     // Interrupt
	9: statusInterrupted, // HITL_Interrupt is terminal, unlike Interrupt (8).
}

var terminalRunStates = map[int]struct{}{
	successRunState:     {},
	failedRunState:      {},
	canceledRunState:    {},
	interruptedRunState: {},
}

func statusForRunState(state int) string {
	if s, ok := runStateStatus[state]; ok {
		return s
	}
	return statusUnknown
}

func isTerminalRunState(state int) bool {
	_, ok := terminalRunStates[state]
	return ok
}

// QueryResultOptions is the command-facing request shape for query-result.
type QueryResultOptions struct {
	ThreadID    string
	RunID       string
	DownloadDir string
}

// QueryResultResult describes the user-facing query-result outcome.
type QueryResultResult struct {
	TeamID       string             `json:"team_id"`
	Status       string             `json:"status,omitempty"`
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
	Draft       *bool  `json:"draft,omitempty"`
	DraftTaskID string `json:"draft_task_id,omitempty"`
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
	RunID        string           `json:"run_id"`
	State        int              `json:"state"`
	ErrorMessage string           `json:"error_message"`
	ErrorMsg     string           `json:"error_msg"`
	Errmsg       string           `json:"errmsg"`
	FailReason   *queryFailReason `json:"fail_reason"`
	EntryList    []queryEntry     `json:"entry_list"`
}

type queryFailReason struct {
	Message string          `json:"message"`
	Code    json.RawMessage `json:"code"`
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
	Draft       *bool  `json:"draft"`
	DraftTaskID string `json:"draft_task_id"`
	Title       string `json:"title"`
	VID         string `json:"vid"`
	AssetID     string `json:"asset_id"`
}

type queryImage struct {
	DownloadURL   string         `json:"url"`
	AssetID       string         `json:"asset_id"`
	PippitAssetID string         `json:"pippit_asset_id"`
	Metadata      queryImageMeta `json:"metadata"`
}

type queryImageMeta struct {
	Format string `json:"format"`
}

func QueryResult(ctx context.Context, opts *QueryResultOptions, runner *common.Runner) (*QueryResultResult, error) {
	if err := validateQueryResultOptions(opts); err != nil {
		return nil, err
	}

	threadResult, queryErr := common.GetThread(ctx, &common.GetThreadOptions{
		ThreadID: opts.ThreadID,
		RunID:    opts.RunID,
	}, runner)
	thread, err := parseQueryThread(threadResult)
	if err != nil {
		if queryErr != nil {
			return queryResultWithoutRun(threadResult, queryErr, opts)
		}
		return nil, fmt.Errorf("查询失败：%w", err)
	}

	run, ok := findQueryRun(thread, opts.RunID)
	if !ok {
		if queryErr != nil {
			return queryResultWithoutRun(threadResult, queryErr, opts)
		}
		return nil, fmt.Errorf("查询失败：未找到 run_id=%s 对应的 Run", opts.RunID)
	}
	logResolvedQueryError(queryErr, opts)
	result := &QueryResultResult{
		TeamID:    common.SpaceTeamID(ctx),
		Status:    statusForRunState(run.State),
		Completed: isTerminalRunState(run.State),
		ThreadID:  firstNonEmpty(thread.ThreadID, opts.ThreadID),
		RunID:     opts.RunID,
		Videos:    []QueryResultVideo{},
		Images:    []QueryResultImage{},
	}
	if run.State != successRunState {
		if run.State == failedRunState {
			message := extractQueryErrorMessage(run)
			if message == "" && queryErr != nil {
				message = queryResultQueryError(queryErr).Error()
			}
			result.ErrorMessage = firstNonEmpty(message, "Run 失败")
		}
		return result, nil
	}

	videos := extractQueryVideos(run)
	images := extractQueryImages(run)
	if len(videos) == 0 && len(images) == 0 {
		return result, fmt.Errorf("下载失败：未找到可下载的产物")
	}

	downloadDir, err := common.ExpandPath(opts.DownloadDir)
	if err != nil {
		return result, fmt.Errorf("下载失败：解析下载目录失败：%w", err)
	}

	usedNames := make(map[string]int, len(videos)+len(images))

	for i, video := range videos {
		if strings.TrimSpace(video.DownloadURL) == "" {
			return result, fmt.Errorf("下载失败：第 %d 个视频产物 download_url 为空", i+1)
		}
		outputPath := filepath.Join(downloadDir, uniqueQueryResultFileName(videoFileName(video, i+1), usedNames))
		download, err := common.DownloadResult(ctx, common.DownloadResultOptions{
			URL:        video.DownloadURL,
			OutputPath: outputPath,
			Workers:    5,
		}, runner)
		if err != nil {
			return result, fmt.Errorf("下载失败：%w", err)
		}
		actualOutputPath := outputPath
		if len(download.Downloaded) > 0 {
			actualOutputPath = download.Downloaded[0]
		} else if len(download.AlreadyExist) > 0 {
			actualOutputPath = download.AlreadyExist[0]
		}
		result.Videos = append(result.Videos, QueryResultVideo{
			DownloadURL: video.DownloadURL,
			OutputPath:  actualOutputPath,
			Draft:       video.Draft,
			DraftTaskID: video.DraftTaskID,
		})
	}

	for i, image := range images {
		if strings.TrimSpace(image.DownloadURL) == "" {
			return result, fmt.Errorf("下载失败：第 %d 个图片产物 download_url 为空", i+1)
		}
		outputPath := filepath.Join(downloadDir, uniqueQueryResultFileName(imageFileName(image, i+1), usedNames))
		download, err := common.DownloadResult(ctx, common.DownloadResultOptions{
			URL:        image.DownloadURL,
			OutputPath: outputPath,
			Workers:    5,
		}, runner)
		if err != nil {
			return result, fmt.Errorf("下载失败：%w", err)
		}
		actualOutputPath := outputPath
		if len(download.Downloaded) > 0 {
			actualOutputPath = download.Downloaded[0]
		} else if len(download.AlreadyExist) > 0 {
			actualOutputPath = download.AlreadyExist[0]
		}
		result.Images = append(result.Images, QueryResultImage{
			DownloadURL: image.DownloadURL,
			OutputPath:  actualOutputPath,
		})
	}

	return result, nil
}

// The legacy endpoint omits data for Failed/Canceled. Only these exact
// endpoint-specific responses prove a terminal Run; other errors (including
// ret=5 "获取会话信息失败") describe a failed query, not a failed Run.
func queryResultWithoutRun(threadResult *common.GetThreadResult, queryErr error, opts *QueryResultOptions) (*QueryResultResult, error) {
	status := statusUnknown
	if threadResult != nil && threadResult.Ret == "5" {
		message := strings.TrimSpace(threadResult.Errmsg)
		switch {
		case message == "创作失败" || strings.HasPrefix(message, "创作失败："):
			status = statusFailed
		case message == "创作已取消":
			status = statusCanceled
		}
	}
	if status == statusUnknown {
		return nil, queryResultQueryError(queryErr)
	}
	logResolvedQueryError(queryErr, opts)
	result := NewQueryResultError(queryResultQueryError(queryErr), opts)
	result.Status = status
	result.Completed = true
	return result, nil
}

// The command logs returned errors. Errors consumed by a resolved Run must
// also retain their original ret/errmsg/log_id for diagnosis.
func logResolvedQueryError(err error, opts *QueryResultOptions) {
	if err != nil {
		_ = common.AppendDailyErrorLog("query-result", err, map[string]string{
			"thread_id": opts.ThreadID,
			"run_id":    opts.RunID,
		})
	}
}

// queryResultQueryError extracts a customer-facing get_thread error. It is used
// when no target Run can be resolved, or as the failed Run's fallback reason.
func queryResultQueryError(err error) error {
	var logErr *common.LogIDError
	if errors.As(err, &logErr) {
		message := getThreadBusinessErrorMessage(logErr.Message)
		if message == "" {
			message = "查询失败"
		}
		if logID := logErr.LogID(); logID != "" {
			return fmt.Errorf("%s log_id=%s", message, logID)
		}
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("查询失败：%w", err)
}

// NewQueryResultError builds the user-facing result when no Run could be
// resolved. The concrete cause stays in error_message.
func NewQueryResultError(err error, opts *QueryResultOptions) *QueryResultResult {
	result := &QueryResultResult{
		Status:       statusUnknown,
		ErrorMessage: err.Error(),
		Videos:       []QueryResultVideo{},
		Images:       []QueryResultImage{},
	}
	if opts != nil {
		result.ThreadID = strings.TrimSpace(opts.ThreadID)
		result.RunID = strings.TrimSpace(opts.RunID)
	}
	return result
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
	if run.FailReason != nil {
		message := strings.TrimSpace(run.FailReason.Message)
		code := rawMessageString(run.FailReason.Code)
		if message != "" {
			if code != "" && code != "0" {
				return fmt.Sprintf("%s (error_code=%s)", message, code)
			}
			return message
		}
	}
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
	if run.FailReason != nil {
		if code := rawMessageString(run.FailReason.Code); code != "" && code != "0" {
			return "error_code=" + code
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
	name := firstNonEmpty(image.PippitAssetID, image.AssetID)
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
