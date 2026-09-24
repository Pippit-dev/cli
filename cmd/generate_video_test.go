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
	assetIDs := []string{"image_asset_1", "image_asset_2", "video_asset_1", "video_asset_2", "audio_asset_1"}
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
			if files[0].Filename == "bgm.wav" && !strings.HasPrefix(files[0].Header.Get("Content-Type"), "audio/") {
				t.Fatalf("audio content type = %q, want audio content type", files[0].Header.Get("Content-Type"))
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
			if param["generate_type"] != float64(1) {
				t.Fatalf("generate_type = %v, want 1", param["generate_type"])
			}
			assertAssetRefs(t, param["images"], []string{"image_asset_1", "image_asset_2"})
			assertAssetRefs(t, param["videos"], []string{"video_asset_1", "video_asset_2"})
			assertAssetRefs(t, param["audios"], []string{"audio_asset_1"})
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
	audio1 := filepath.Join(cwd, "bgm.wav")
	for _, path := range []string{image1, image2, video1, video2, audio1} {
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
		"--audio", audio1,
		"--duration", "5",
		"--ratio", "9:16",
		"--model", "seedance2.0_vision",
		"--resolution", "720p",
		"--generate-type", "1",
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
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			var body map[string]any
			if err := sonic.Unmarshal(data, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			param, ok := body["video_part_tool_param"].(map[string]any)
			if !ok {
				t.Fatalf("video_part_tool_param = %#v, want object", body["video_part_tool_param"])
			}
			if param["generate_type"] != float64(99) {
				t.Fatalf("generate_type = %v, want 99", param["generate_type"])
			}
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
		"--generate-type", "99",
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

func TestGenerateVideoDraftParameters(t *testing.T) {
	for _, tt := range []struct {
		name   string
		args   []string
		want   map[string]any
		absent []string
	}{
		{
			name:   "preview with explicit zero values",
			args:   []string{"--draft", "--prompt", "cat", "--seed", "0", "--generate-type", "0", "--task-type", "reference"},
			want:   map[string]any{"draft": true, "prompt": "cat", "seed": float64(0), "generate_type": float64(0), "task_type": "reference"},
			absent: []string{"draft_task_id", "resolution", "ratio", "duration_sec"},
		},
		{
			name:   "final with ID only",
			args:   []string{"--draft-task-id", "cgt-example-draft"},
			want:   map[string]any{"draft_task_id": "cgt-example-draft", "prompt": ""},
			absent: []string{"draft", "resolution", "ratio", "duration_sec", "seed", "task_type", "generate_type", "images", "videos", "audios"},
		},
		{
			name:   "explicit false",
			args:   []string{"--draft=false", "--draft-task-id", "cgt-example-draft", "--seed", "-1"},
			want:   map[string]any{"draft": false, "draft_task_id": "cgt-example-draft", "seed": float64(-1)},
			absent: []string{"resolution", "ratio", "duration_sec"},
		},
		{
			name: "conflicting parameters are left to the service",
			args: []string{"--draft", "--draft-task-id", "cgt-example-draft", "--task-type", "future-mode", "--duration", "-1", "--ratio", "adaptive"},
			want: map[string]any{"draft": true, "draft_task_id": "cgt-example-draft", "task_type": "future-mode", "duration_sec": float64(-1), "ratio": "adaptive"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			submitted := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/biz/v1/skill/submit_run" {
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				body := decodeJSON(t, data)
				param, ok := body["video_part_tool_param"].(map[string]any)
				if !ok {
					t.Fatalf("missing video_part_tool_param: %#v", body)
				}
				if param["model"] != "Seedance_2.5_draft" || body["agent_name"] != "pippit_video_part_agent" {
					t.Fatalf("unexpected route/model: %#v", body)
				}
				for key, want := range tt.want {
					if param[key] != want {
						t.Fatalf("%s = %#v, want %#v", key, param[key], want)
					}
				}
				for _, key := range tt.absent {
					if _, exists := param[key]; exists {
						t.Fatalf("%s must stay omitted: %#v", key, param)
					}
				}
				submitted = true
				_, _ = w.Write([]byte(`{"ret":"0","data":{"run":{"thread_id":"thread_123","run_id":"run_456"}}}`))
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			root.SetArgs(append([]string{"generate-video", "--model", "Seedance_2.5_draft"}, tt.args...))
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute(): %v, stderr=%s", err, stderr.String())
			}
			if !submitted {
				t.Fatal("request not submitted")
			}
		})
	}
}

func TestGenerateVideoAcceptsReferencesBeyondFormerLimits(t *testing.T) {
	cwd := chdirTemp(t)
	args := []string{"generate-video", "--prompt", "x"}
	var assetIDs []string
	for _, media := range []struct {
		flag  string
		ext   string
		count int
	}{
		{"image", ".jpg", 10},
		{"video", ".mp4", 4},
		{"audio", ".mp3", 4},
	} {
		for _, name := range mediaPaths(media.flag, media.ext, media.count) {
			path := filepath.Join(cwd, name)
			if err := os.WriteFile(path, []byte("media-data"), 0o644); err != nil {
				t.Fatalf("WriteFile(%s): %v", path, err)
			}
			args = append(args, "--"+media.flag, path)
			assetIDs = append(assetIDs, name)
		}
	}

	uploadIndex := 0
	submitted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/upload_file":
			if uploadIndex >= len(assetIDs) {
				t.Fatalf("unexpected upload %d", uploadIndex)
			}
			_, _ = w.Write([]byte(`{"ret":"0","data":{"pippit_asset_id":"` + assetIDs[uploadIndex] + `"}}`))
			uploadIndex++
		case "/api/biz/v1/skill/submit_run":
			if uploadIndex != len(assetIDs) {
				t.Fatalf("uploaded %d references, want %d", uploadIndex, len(assetIDs))
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			var body map[string]any
			if err := sonic.Unmarshal(data, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			param, ok := body["video_part_tool_param"].(map[string]any)
			if !ok {
				t.Fatalf("video_part_tool_param = %#v, want object", body["video_part_tool_param"])
			}
			assertAssetRefs(t, param["images"], assetIDs[:10])
			assertAssetRefs(t, param["videos"], assetIDs[10:14])
			assertAssetRefs(t, param["audios"], assetIDs[14:])
			submitted = true
			_, _ = w.Write([]byte(`{"ret":"0","data":{"run":{"thread_id":"thread_123","run_id":"run_456"}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if !submitted {
		t.Fatal("generate-video did not submit references")
	}
}

func TestGenerateVideoRejectsUnsupportedAudioExtension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive request when audio extension is invalid")
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"generate-video",
		"--prompt", "x",
		"--audio", "bgm.wma",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want audio extension validation")
	}
	if !strings.Contains(err.Error(), `不支持的音频文件后缀 ".wma"`) {
		t.Fatalf("error = %q, want audio extension validation", err)
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
	for _, unwanted := range []string{"vid", "asset_id", "title", "draft", "draft_task_id"} {
		if _, ok := video[unwanted]; ok {
			t.Fatalf("video = %#v, should not contain %s", video, unwanted)
		}
	}
	assertFileContent(t, outputPath, "video-data")
}

func TestQueryResultPreservesDraftMetadata(t *testing.T) {
	for _, draft := range []bool{true, false} {
		t.Run(map[bool]string{true: "preview", false: "final"}[draft], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/biz/v1/skill/get_thread":
					videoJSON, err := sonic.Marshal(map[string]any{"video": map[string]any{
						"download_url": serverURL(r) + "/video.mp4",
						"draft":        draft, "draft_task_id": "cgt-example-draft",
					}})
					if err != nil {
						t.Fatal(err)
					}
					// Agent parts carry their data as a JSON string.
					encoded, err := sonic.Marshal(string(videoJSON))
					if err != nil {
						t.Fatal(err)
					}
					_, _ = w.Write([]byte(`{"ret":"0","data":{"thread":{"thread_id":"draft_thread","run_list":[{"run_id":"draft_run","state":3,"entry_list":[{"artifact":{"content":[{"sub_type":"biz/x_data_video","data":` + string(encoded) + `}]}}]}]}}}`))
				case "/video.mp4":
					_, _ = w.Write([]byte("video-data"))
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			root.SetArgs([]string{"query-result", "--thread-id", "draft_thread", "--run-id", "draft_run", "--download-dir", t.TempDir()})
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute(): %v", err)
			}
			got := decodeJSON(t, stdout.Bytes())
			if got["completed"] != true || got["error_message"] != "" {
				t.Fatalf("unexpected result: %#v", got)
			}
			videos, ok := got["videos"].([]any)
			if !ok || len(videos) != 1 {
				t.Fatalf("unexpected videos: %#v", got)
			}
			video := videos[0].(map[string]any)
			if video["draft"] != draft || video["draft_task_id"] != "cgt-example-draft" {
				t.Fatalf("draft metadata was lost: %#v", video)
			}
			assertFileContent(t, video["output_path"].(string), "video-data")
		})
	}
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

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	got := decodeJSON(t, stdout.Bytes())
	if got["completed"] != false {
		t.Fatalf("completed = %v, want false", got["completed"])
	}
	if got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
		t.Fatalf("ids = (%v, %v), want thread/run ids", got["thread_id"], got["run_id"])
	}
	if got["error_message"] != "下载失败：未找到可下载的产物" {
		t.Fatalf("error_message = %v, want no downloadable video error", got["error_message"])
	}
	videos, ok := got["videos"].([]any)
	if !ok || len(videos) != 0 {
		t.Fatalf("videos = %#v, want empty", got["videos"])
	}
}

func TestQueryResultValidationErrorReturnsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, "http://127.0.0.1")
	root.SetArgs([]string{
		"query-result",
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
	if got["thread_id"] != "" || got["run_id"] != "run_456" {
		t.Fatalf("ids = (%v, %v), want empty thread_id and run_id", got["thread_id"], got["run_id"])
	}
	if got["error_message"] != "查询失败：缺少必填参数 --thread-id" {
		t.Fatalf("error_message = %v, want validation error", got["error_message"])
	}
	videos, ok := got["videos"].([]any)
	if !ok || len(videos) != 0 {
		t.Fatalf("videos = %#v, want empty", got["videos"])
	}
}

func TestQueryResultGetThreadBusinessErrorReturnsErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/get_thread":
			_, _ = w.Write([]byte(`{"ret":"5","errmsg":"创作失败：暂时无法生成","log_id":"log_456"}`))
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
	if got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
		t.Fatalf("ids = (%v, %v), want thread/run ids", got["thread_id"], got["run_id"])
	}
	if got["error_message"] != "创作失败：暂时无法生成 log_id=log_456" {
		t.Fatalf("error_message = %v, want get_thread business error", got["error_message"])
	}
	videos, ok := got["videos"].([]any)
	if !ok || len(videos) != 0 {
		t.Fatalf("videos = %#v, want empty", got["videos"])
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
