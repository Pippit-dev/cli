package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
)

func TestGenerateVideo(t *testing.T) {
	assetIDs := []string{"image_asset_1", "image_asset_2", "video_asset_1", "video_asset_2"}
	uploadIndex := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want test bearer token", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/biz/v1/skill/upload_file":
			if r.Method != http.MethodPost {
				t.Fatalf("upload method = %s, want POST", r.Method)
			}
			if uploadIndex >= len(assetIDs) {
				t.Fatalf("unexpected upload %d", uploadIndex)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("ParseMultipartForm(): %v", err)
			}
			files := r.MultipartForm.File["file"]
			if len(files) != 1 {
				t.Fatalf("file parts = %d, want 1", len(files))
			}
			uploadIndex++
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"pippit_asset_id":"` + assetIDs[uploadIndex-1] + `"}}`))
		case "/api/biz/v1/skill/submit_run":
			if uploadIndex != len(assetIDs) {
				t.Fatalf("submit called after %d uploads, want %d", uploadIndex, len(assetIDs))
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			var body map[string]any
			if err := sonic.Unmarshal(data, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["agent_name"] != "pippit_video_part_agent" {
				t.Fatalf("agent_name = %v, want video part agent", body["agent_name"])
			}
			if body["message"] != "做个小猫视频" {
				t.Fatalf("message = %v, want submitted prompt", body["message"])
			}
			param, ok := body["video_part_tool_param"].(map[string]any)
			if !ok {
				t.Fatalf("video_part_tool_param = %#v, want object", body["video_part_tool_param"])
			}
			if param["prompt"] != "做个小猫视频" {
				t.Fatalf("prompt = %v, want submitted prompt", param["prompt"])
			}
			if param["duration_sec"] != float64(5) {
				t.Fatalf("duration_sec = %v, want 5", param["duration_sec"])
			}
			if param["ratio"] != "9:16" || param["model"] != "seedance2.0_vision" || param["resolution"] != "720p" {
				t.Fatalf("param = %#v, want ratio/model/resolution", param)
			}
			assertAssetRefs(t, param["images"], []string{"image_asset_1", "image_asset_2"})
			assertAssetRefs(t, param["videos"], []string{"video_asset_1", "video_asset_2"})
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"run":{"thread_id":"thread_123","run_id":"run_456"},"web_thread_link":"https://xyq.example/thread_123"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cwd := chdirTemp(t)
	image1 := filepath.Join(cwd, "cat1.jpg")
	image2 := filepath.Join(cwd, "cat2.jpg")
	video1 := filepath.Join(cwd, "video1.mp4")
	video2 := filepath.Join(cwd, "video2.mp4")
	for _, path := range []string{image1, image2, video1, video2} {
		if err := os.WriteFile(path, []byte("media-data"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"generate-video",
		"--prompt", "做个小猫视频",
		"--image", image1,
		"--image", image2,
		"--video", video1,
		"--video", video2,
		"--duration", "5",
		"--ratio", "9:16",
		"--model", "seedance2.0_vision",
		"--resolution", "720p",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
		t.Fatalf("output = %#v, want thread and run IDs", got)
	}
}

func TestGenerateVideoSkipsSemanticValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/submit_run":
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"run":{"thread_id":"thread_123","run_id":"run_456"}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"generate-video",
		"--prompt", "x",
		"--duration", "1",
		"--ratio", "1:1",
		"--model", "bad_model",
		"--resolution", "bad_resolution",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
}

func TestGenerateVideoRequiresPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive request without prompt")
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"generate-video",
		"--duration", "1",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want prompt validation")
	}
	if !strings.Contains(err.Error(), "缺少必填参数 --prompt") {
		t.Fatalf("error = %q, want prompt validation", err)
	}
}

func TestGenerateVideoRejectsTooManyImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive request when image count is invalid")
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	args := []string{"generate-video", "--prompt", "x"}
	for _, path := range mediaPaths("image", ".jpg", 10) {
		args = append(args, "--image", path)
	}
	root.SetArgs(args)

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want image count validation")
	}
	if !strings.Contains(err.Error(), "参考图片最多支持 9 个，当前传入 10 个") {
		t.Fatalf("error = %q, want image count validation", err)
	}
}

func TestGenerateVideoRejectsTooManyVideos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive request when video count is invalid")
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	args := []string{"generate-video", "--prompt", "x"}
	for _, path := range mediaPaths("video", ".mp4", 4) {
		args = append(args, "--video", path)
	}
	root.SetArgs(args)

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want video count validation")
	}
	if !strings.Contains(err.Error(), "参考视频最多支持 3 个，当前传入 4 个") {
		t.Fatalf("error = %q, want video count validation", err)
	}
}

func TestGenerateVideoSubmitRunErrorIncludesLogID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/submit_run":
			_, _ = w.Write([]byte(`{"ret":"16008","errmsg":"提交Run任务失败","log_id":"log_123"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"generate-video",
		"--prompt", "x",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want submit_run error")
	}
	if !strings.Contains(err.Error(), "log_id=log_123") {
		t.Fatalf("error = %q, want log_id", err)
	}
}

func TestGenerateVideoErrorLogIncludesLogID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/submit_run":
			_, _ = w.Write([]byte(`{"ret":"16008","errmsg":"提交Run任务失败","log_id":"log_123"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	clearDailyErrorLog(t)
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"generate-video",
		"--prompt", "x",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want submit_run error")
	}
	entries := readDailyErrorLog(t)
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1: %#v", len(entries), entries)
	}
	fields, ok := entries[0]["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields = %#v, want object", entries[0]["fields"])
	}
	if fields["log_id"] != "log_123" {
		t.Fatalf("log_id = %v, want log_123", fields["log_id"])
	}
}

func TestQueryResultDownloadsCompletedVideo(t *testing.T) {
	var requestedDownload bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want test bearer token", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/biz/v1/skill/get_thread":
			if r.Method != http.MethodPost {
				t.Fatalf("get_thread method = %s, want POST", r.Method)
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			var body map[string]any
			if err := sonic.Unmarshal(data, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["thread_id"] != "thread_123" {
				t.Fatalf("thread_id = %v, want thread_123", body["thread_id"])
			}
			if body["run_id"] != "run_456" {
				t.Fatalf("run_id = %v, want run_456", body["run_id"])
			}
			if _, ok := body["version"]; ok {
				t.Fatalf("version = %v, want omitted", body["version"])
			}
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"thread":{"thread_id":"thread_123","run_list":[{"run_id":"run_456","state":3,"entry_list":[{"artifact":{"content":[{"sub_type":"biz/x_data_prompt_text","data":"做个小猫视频"},{"sub_type":"biz/x_data_video","data":"{\"video\":{\"download_url\":\"` + serverURL(r) + `/video.mp4\",\"title\":\"cat_video\",\"vid\":\"cat_vid\"}}"}]}}]}]}}}`))
		case "/video.mp4":
			requestedDownload = true
			if r.Method != http.MethodGet {
				t.Fatalf("download method = %s, want GET", r.Method)
			}
			_, _ = w.Write([]byte("video-data"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	downloadDir := filepath.Join(t.TempDir(), "downloads")
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"query-result",
		"--thread-id", "thread_123",
		"--run-id", "run_456",
		"--download-dir", downloadDir,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	outputPath := filepath.Join(downloadDir, "cat_vid.mp4")
	downloadURL := server.URL + "/video.mp4"
	if !requestedDownload {
		t.Fatal("download endpoint was not requested")
	}
	got := decodeJSON(t, stdout.Bytes())
	if got["completed"] != true {
		t.Fatalf("completed = %v, want true", got["completed"])
	}
	if got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
		t.Fatalf("ids = (%v, %v), want thread/run ids", got["thread_id"], got["run_id"])
	}
	if _, ok := got["state"]; ok {
		t.Fatalf("state should not be returned: %#v", got)
	}
	if got["error_message"] != "" {
		t.Fatalf("error_message = %v, want empty", got["error_message"])
	}
	videos, ok := got["videos"].([]any)
	if !ok || len(videos) != 1 {
		t.Fatalf("videos = %#v, want one video", got["videos"])
	}
	video, ok := videos[0].(map[string]any)
	if !ok {
		t.Fatalf("video = %#v, want object", videos[0])
	}
	if video["download_url"] != downloadURL || video["output_path"] != outputPath {
		t.Fatalf("video = %#v, want download_url/output_path", video)
	}
	for _, unwanted := range []string{"vid", "asset_id", "title"} {
		if _, ok := video[unwanted]; ok {
			t.Fatalf("video = %#v, should not contain %s", video, unwanted)
		}
	}
	assertFileContent(t, outputPath, "video-data")
}

func TestQueryResultIgnoresVideoDataWithoutVideoSubType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/get_thread":
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"thread":{"thread_id":"thread_123","run_list":[{"run_id":"run_456","state":3,"entry_list":[{"artifact":{"content":[{"data":"{\"video\":{\"download_url\":\"` + serverURL(r) + `/video.mp4\",\"title\":\"cat_video\"}}"}]}}]}]}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"query-result",
		"--thread-id", "thread_123",
		"--run-id", "run_456",
		"--download-dir", t.TempDir(),
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want no downloadable video error")
	}
	if !strings.Contains(err.Error(), "下载失败：未找到可下载的视频产物") {
		t.Fatalf("error = %q, want no downloadable video error", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestQueryResultErrorLogIncludesLogID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/get_thread":
			_, _ = w.Write([]byte(`{"ret":"5","errmsg":"创作失败","log_id":"log_456"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	clearDailyErrorLog(t)
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"query-result",
		"--thread-id", "thread_123",
		"--run-id", "run_456",
		"--download-dir", t.TempDir(),
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want get_thread error")
	}
	entries := readDailyErrorLog(t)
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1: %#v", len(entries), entries)
	}
	fields, ok := entries[0]["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields = %#v, want object", entries[0]["fields"])
	}
	if fields["log_id"] != "log_456" {
		t.Fatalf("log_id = %v, want log_456", fields["log_id"])
	}
	if fields["thread_id"] != "thread_123" || fields["run_id"] != "run_456" {
		t.Fatalf("fields = %#v, want thread/run ids", fields)
	}
}

func TestQueryResultFailedReturnsErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/get_thread":
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"thread":{"thread_id":"thread_123","run_list":[{"run_id":"run_456","state":4,"entry_list":[{"artifact":{"content":[{"sub_type":"biz/x_data_video","data":"{\"error_message\":\"生成失败\",\"error_code\":\"11001\"}"}]}}]}]}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"query-result",
		"--thread-id", "thread_123",
		"--run-id", "run_456",
		"--download-dir", t.TempDir(),
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	got := decodeJSON(t, stdout.Bytes())
	if got["completed"] != true {
		t.Fatalf("completed = %v, want true", got["completed"])
	}
	if _, ok := got["state"]; ok {
		t.Fatalf("state should not be returned: %#v", got)
	}
	if got["error_message"] != "生成失败 (error_code=11001)" {
		t.Fatalf("error_message = %v, want failure message", got["error_message"])
	}
	videos, ok := got["videos"].([]any)
	if !ok || len(videos) != 0 {
		t.Fatalf("videos = %#v, want empty", got["videos"])
	}
}

func TestQueryResultPendingDoesNotDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/get_thread":
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"thread":{"thread_id":"thread_123","run_list":[{"run_id":"run_456","state":1}]}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"query-result",
		"--thread-id", "thread_123",
		"--run-id", "run_456",
		"--download-dir", t.TempDir(),
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	got := decodeJSON(t, stdout.Bytes())
	if got["completed"] != false {
		t.Fatalf("completed = %v, want false", got["completed"])
	}
	if _, ok := got["state"]; ok {
		t.Fatalf("state should not be returned: %#v", got)
	}
	if got["error_message"] != "" {
		t.Fatalf("error_message = %v, want empty", got["error_message"])
	}
	videos, ok := got["videos"].([]any)
	if !ok || len(videos) != 0 {
		t.Fatalf("videos = %#v, want empty", got["videos"])
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

func assertAssetRefs(t *testing.T, got any, want []string) {
	t.Helper()
	items, ok := got.([]any)
	if !ok || len(items) != len(want) {
		t.Fatalf("asset refs = %#v, want %v", got, want)
	}
	for i, item := range items {
		ref, ok := item.(map[string]any)
		if !ok || ref["pippit_asset_id"] != want[i] {
			t.Fatalf("asset refs[%d] = %#v, want %s", i, item, want[i])
		}
	}
}

func mediaPaths(prefix string, ext string, count int) []string {
	paths := make([]string, 0, count)
	for i := 0; i < count; i++ {
		paths = append(paths, prefix+string(rune('a'+i))+ext)
	}
	return paths
}
