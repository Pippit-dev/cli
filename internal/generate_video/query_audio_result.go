package generate_video

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const canceledAudioRunState = 5

// QueryAudioResultResult describes the user-facing query-result outcome.
type QueryAudioResultResult struct {
	Completed    bool               `json:"completed"`
	ThreadID     string             `json:"thread_id"`
	RunID        string             `json:"run_id"`
	ErrorMessage string             `json:"error_message"`
	Videos       []QueryResultVideo `json:"videos"`
	Images       []QueryResultImage `json:"images"`
	Audios       []QueryResultAudio `json:"audios"`
}

// QueryResultAudio describes a downloaded audio. Duration is in seconds, when available.
type QueryResultAudio struct {
	DownloadURL   string   `json:"download_url"`
	OutputPath    string   `json:"output_path"`
	Name          string   `json:"name,omitempty"`
	PippitAssetID string   `json:"pippit_asset_id,omitempty"`
	Duration      *float64 `json:"duration,omitempty"`
}

type audioQueryThread struct {
	ThreadID string          `json:"thread_id"`
	RunList  []audioQueryRun `json:"run_list"`
}

type audioQueryRun struct {
	RunID        string               `json:"run_id"`
	State        int                  `json:"state"`
	ErrorMessage string               `json:"error_message"`
	ErrorMsg     string               `json:"error_msg"`
	Errmsg       string               `json:"errmsg"`
	FailReason   audioQueryFailReason `json:"fail_reason"`
	EntryList    []audioQueryEntry    `json:"entry_list"`
}

type audioQueryFailReason struct {
	Message         string          `json:"message"`
	FallbackMessage string          `json:"fallback_message"`
	Code            json.RawMessage `json:"code"`
}

type audioQueryEntry struct {
	Artifact audioQueryArtifact `json:"artifact"`
}

type audioQueryArtifact struct {
	Content []audioQueryContent `json:"content"`
}

type audioQueryContent struct {
	SubType string                `json:"sub_type"`
	Data    audioQueryContentData `json:"data"`
}

type audioQueryContentData struct {
	Video        *queryVideo     `json:"video"`
	Image        *queryImage     `json:"image"`
	Audio        *queryAudio     `json:"audio"`
	ErrorMessage string          `json:"error_message"`
	ErrorCode    json.RawMessage `json:"error_code"`
}

type queryAudio struct {
	DownloadURL   string         `json:"url"`
	Name          string         `json:"name"`
	PippitAssetID string         `json:"pippit_asset_id"`
	Metadata      queryAudioMeta `json:"metadata"`
}

type queryAudioMeta struct {
	Format   string   `json:"format"`
	Duration *float64 `json:"duration"`
}

func QueryAudioResult(ctx context.Context, opts *QueryResultOptions, runner *common.Runner) (*QueryAudioResultResult, error) {
	if err := validateQueryResultOptions(opts); err != nil {
		return nil, err
	}

	threadResult, err := common.GetThread(ctx, &common.GetThreadOptions{
		ThreadID:          opts.ThreadID,
		RunID:             opts.RunID,
		PreserveErrorData: true,
	}, runner)
	if err != nil {
		if result, ok := audioQueryResultFromGetThreadBusinessError(err, opts); ok {
			return result, nil
		}
		return nil, fmt.Errorf("查询失败：%w", err)
	}

	thread, err := parseAudioQueryThread(threadResult)
	if err != nil {
		return nil, fmt.Errorf("查询失败：%w", err)
	}

	run, ok := findAudioQueryRun(thread, opts.RunID)
	if !ok {
		return nil, fmt.Errorf("查询失败：未找到 run_id=%s 对应的 Run", opts.RunID)
	}
	if run.State != successRunState {
		result := &QueryAudioResultResult{
			Completed: run.State == failedRunState || run.State == canceledAudioRunState,
			ThreadID:  firstNonEmpty(thread.ThreadID, opts.ThreadID),
			RunID:     opts.RunID,
			Videos:    []QueryResultVideo{},
			Images:    []QueryResultImage{},
			Audios:    []QueryResultAudio{},
		}
		if run.State == failedRunState {
			result.ErrorMessage = firstNonEmpty(extractAudioQueryErrorMessage(run), "Run 失败")
		} else if run.State == canceledAudioRunState {
			result.ErrorMessage = firstNonEmpty(extractAudioQueryErrorMessage(run), "Run 已取消")
		}
		return result, nil
	}

	videos := extractAudioQueryVideos(run)
	images := extractAudioQueryImages(run)
	audios := extractQueryAudios(run)
	if len(videos) == 0 && len(images) == 0 && len(audios) == 0 {
		return nil, fmt.Errorf("下载失败：未找到可下载的产物")
	}

	downloadDir, err := common.ExpandPath(opts.DownloadDir)
	if err != nil {
		return nil, fmt.Errorf("下载失败：解析下载目录失败：%w", err)
	}

	usedNames := make(map[string]int, len(videos)+len(images)+len(audios))

	resultVideos := make([]QueryResultVideo, 0, len(videos))
	for i, video := range videos {
		if strings.TrimSpace(video.DownloadURL) == "" {
			return nil, fmt.Errorf("下载失败：第 %d 个视频产物 download_url 为空", i+1)
		}
		outputPath := filepath.Join(downloadDir, uniqueAudioQueryResultFileName(videoFileName(video, i+1), usedNames))
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
		outputPath := filepath.Join(downloadDir, uniqueAudioQueryResultFileName(imageFileName(image, i+1), usedNames))
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

	resultAudios := make([]QueryResultAudio, 0, len(audios))
	for i, audio := range audios {
		if strings.TrimSpace(audio.DownloadURL) == "" {
			return nil, fmt.Errorf("下载失败：第 %d 个音频产物 url 为空", i+1)
		}
		outputPath := filepath.Join(downloadDir, uniqueAudioQueryResultFileName(audioFileName(audio, i+1), usedNames))
		if _, err := common.DownloadResult(ctx, common.DownloadResultOptions{
			URL: audio.DownloadURL, OutputPath: outputPath,
		}, runner); err != nil {
			return nil, fmt.Errorf("下载失败：%w", err)
		}
		resultAudios = append(resultAudios, QueryResultAudio{
			DownloadURL: audio.DownloadURL, OutputPath: outputPath, Name: audio.Name,
			PippitAssetID: audio.PippitAssetID, Duration: audio.Metadata.Duration,
		})
	}

	return &QueryAudioResultResult{
		Completed: true,
		ThreadID:  firstNonEmpty(thread.ThreadID, opts.ThreadID),
		RunID:     opts.RunID,
		Videos:    resultVideos,
		Images:    resultImages,
		Audios:    resultAudios,
	}, nil
}

func audioQueryResultFromGetThreadBusinessError(err error, opts *QueryResultOptions) (*QueryAudioResultResult, bool) {
	var logErr *common.LogIDError
	if !errors.As(err, &logErr) {
		return nil, false
	}
	message := getThreadBusinessErrorMessage(logErr.Message)
	if message == "" {
		message = "未知错误"
	}
	completed := false
	// Nonzero ret can represent either a query failure or an observed Run failure.
	// Only a matching structured Run in the error payload can establish a terminal state.
	thread, parseErr := parseAudioQueryThread(&common.GetThreadResult{RawData: logErr.RawData})
	if parseErr == nil && (thread.ThreadID == "" || thread.ThreadID == opts.ThreadID) {
		if run, ok := findAudioQueryRun(thread, opts.RunID); ok && (run.State == failedRunState || run.State == canceledAudioRunState) {
			completed = true
			message = firstNonEmpty(extractAudioQueryErrorMessage(run), message)
		}
	}
	if !completed {
		message = "查询失败：" + message
	}
	if logID := logErr.LogID(); logID != "" {
		message = fmt.Sprintf("%s log_id=%s", message, logID)
	}
	return &QueryAudioResultResult{
		Completed:    completed,
		ThreadID:     opts.ThreadID,
		RunID:        opts.RunID,
		ErrorMessage: message,
		Videos:       []QueryResultVideo{},
		Images:       []QueryResultImage{},
		Audios:       []QueryResultAudio{},
	}, true
}

func parseAudioQueryThread(result *common.GetThreadResult) (*audioQueryThread, error) {
	if result == nil {
		return nil, fmt.Errorf("get_thread 响应为空")
	}
	if len(result.RawData) > 0 {
		var data map[string]json.RawMessage
		if err := json.Unmarshal(result.RawData, &data); err == nil {
			if raw := data["thread"]; len(raw) > 0 {
				if thread, ok := decodeAudioQueryThread(raw); ok {
					return thread, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("get_thread 响应中未找到 data.thread")
}

func decodeAudioQueryThread(raw []byte) (*audioQueryThread, bool) {
	var thread audioQueryThread
	if err := json.Unmarshal(raw, &thread); err != nil {
		return nil, false
	}
	if thread.ThreadID == "" && len(thread.RunList) == 0 {
		return nil, false
	}
	return &thread, true
}

func findAudioQueryRun(thread *audioQueryThread, runID string) (audioQueryRun, bool) {
	for _, run := range thread.RunList {
		if run.RunID == runID {
			return run, true
		}
	}
	return audioQueryRun{}, false
}

func (data *audioQueryContentData) UnmarshalJSON(raw []byte) error {
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
	type alias audioQueryContentData
	return json.Unmarshal(raw, (*alias)(data))
}

func extractAudioQueryVideos(run audioQueryRun) []queryVideo {
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

func extractAudioQueryImages(run audioQueryRun) []queryImage {
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

func extractQueryAudios(run audioQueryRun) []queryAudio {
	audios := make([]queryAudio, 0)
	for _, entry := range run.EntryList {
		for _, content := range entry.Artifact.Content {
			if content.SubType == "biz/x_data_audio" && content.Data.Audio != nil {
				audios = append(audios, *content.Data.Audio)
			}
		}
	}
	return audios
}

func extractAudioQueryErrorMessage(run audioQueryRun) string {
	if message := firstNonEmpty(run.ErrorMessage, run.ErrorMsg, run.Errmsg); message != "" {
		return message
	}
	if message := firstNonEmpty(run.FailReason.Message, run.FailReason.FallbackMessage); message != "" {
		if code := rawMessageString(run.FailReason.Code); code != "" && code != "0" {
			return fmt.Sprintf("%s (error_code=%s)", message, code)
		}
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

func audioFileName(audio queryAudio, index int) string {
	name := firstNonEmpty(audio.PippitAssetID, audio.Name, "audio_"+strconv.Itoa(index))
	name = sanitizeFileName(name)
	ext := normalizeAudioFormatExt(audio.Metadata.Format)
	if ext == "" {
		if parsed, err := url.Parse(audio.DownloadURL); err == nil {
			ext = normalizeAudioFormatExt(filepath.Ext(parsed.Path))
		}
	}
	if ext == "" {
		ext = normalizeAudioFormatExt(filepath.Ext(audio.Name))
	}
	if ext == "" {
		ext = "audio"
	}
	if normalizeAudioFormatExt(filepath.Ext(name)) != "" {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name + "." + ext
}

// Only known audio formats can become a file extension; URL queries are never used.
func normalizeAudioFormatExt(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	format = strings.TrimPrefix(format, "audio/")
	format = strings.TrimPrefix(format, ".")
	switch format {
	case "mpeg":
		return "mp3"
	case "x-wav", "wave":
		return "wav"
	case "ogg_opus":
		return "ogg"
	case "mp3", "wav", "pcm", "m4a", "aac", "flac", "ogg", "opus":
		return format
	default:
		return ""
	}
}

func uniqueAudioQueryResultFileName(name string, used map[string]int) string {
	count := used[name] + 1
	if count == 1 {
		used[name] = count
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for {
		candidate := fmt.Sprintf("%s-%d%s", base, count, ext)
		if used[candidate] == 0 {
			used[name] = count
			used[candidate] = 1
			return candidate
		}
		count++
	}
}
