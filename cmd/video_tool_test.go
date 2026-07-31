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

func TestVideoSuperResolution(t *testing.T) {
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want test bearer token", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/biz/v1/skill/upload_file":
			assertSingleUploadedVideo(t, r, "source.mp4")
			uploaded = true
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"pippit_asset_id":"video_asset_1"}}`))
		case "/api/biz/v1/skill/submit_run":
			if !uploaded {
				t.Fatal("submit called before upload")
			}
			body := decodeRequestBody(t, r)
			assertVideoToolBaseRequest(t, body, "提升视频清晰度")
			miniTool := videoMiniToolParam(t, body)
			if miniTool["tool_name"] != "video_super_resolution" {
				t.Fatalf("tool_name = %v, want video_super_resolution", miniTool["tool_name"])
			}
			toolParam := requiredObject(t, miniTool, "tool_param")
			superResolution := requiredObject(t, toolParam, "video_super_resolution_tool_param")
			if superResolution["tool_version"] != "professional_v1" {
				t.Fatalf("tool_version = %v, want professional_v1", superResolution["tool_version"])
			}
			if superResolution["output_resolution"] != "2k" {
				t.Fatalf("output_resolution = %v, want 2k", superResolution["output_resolution"])
			}
			assertNestedVideoAsset(t, superResolution, "video_asset_1")
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"run":{"thread_id":"thread_sr","run_id":"run_sr"},"web_thread_link":"https://xyq.example/thread_sr"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	video := writeTestVideo(t, "source.mp4")
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"video-super-resolution",
		"--video", video,
		"--output-resolution", "2k",
		"--tool-version", "professional_v1",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	got := decodeJSON(t, stdout.Bytes())
	if got["thread_id"] != "thread_sr" || got["run_id"] != "run_sr" {
		t.Fatalf("output = %#v, want super-resolution thread and run IDs", got)
	}
}

func TestEraseVideoSubtitle(t *testing.T) {
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/upload_file":
			assertSingleUploadedVideo(t, r, "subtitle.mov")
			uploaded = true
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"pippit_asset_id":"video_asset_2"}}`))
		case "/api/biz/v1/skill/submit_run":
			if !uploaded {
				t.Fatal("submit called before upload")
			}
			body := decodeRequestBody(t, r)
			assertVideoToolBaseRequest(t, body, "擦除视频字幕")
			miniTool := videoMiniToolParam(t, body)
			if miniTool["tool_name"] != "erase_video_subtitle" {
				t.Fatalf("tool_name = %v, want erase_video_subtitle", miniTool["tool_name"])
			}
			toolParam := requiredObject(t, miniTool, "tool_param")
			eraseSubtitle := requiredObject(t, toolParam, "erase_video_subtitle_tool_param")
			if _, ok := eraseSubtitle["tool_version"]; ok {
				t.Fatalf("tool_version must be omitted: %#v", eraseSubtitle)
			}
			assertNestedVideoAsset(t, eraseSubtitle, "video_asset_2")
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"run":{"thread_id":"thread_erase","run_id":"run_erase"}}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	video := writeTestVideo(t, "subtitle.mov")
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"erase-video-subtitle",
		"--video", video,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	got := decodeJSON(t, stdout.Bytes())
	if got["thread_id"] != "thread_erase" || got["run_id"] != "run_erase" {
		t.Fatalf("output = %#v, want erase-subtitle thread and run IDs", got)
	}
}

func TestEraseVideoSubtitleDoesNotExposeToolVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, "http://127.0.0.1")
	root.SetArgs([]string{
		"erase-video-subtitle",
		"--video", "source.mp4",
		"--tool-version", "custom_version",
	})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "未知参数: --tool-version") {
		t.Fatalf("Execute() error = %v, want unknown --tool-version", err)
	}
}

func TestVideoToolErrorLogsIncludeVideoPath(t *testing.T) {
	tests := []struct {
		name string
		args func(string) []string
	}{
		{
			name: "super resolution",
			args: func(path string) []string {
				return []string{"video-super-resolution", "--video", path, "--output-resolution", "1080p"}
			},
		},
		{
			name: "erase subtitle",
			args: func(path string) []string {
				return []string{"erase-video-subtitle", "--video", path}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearDailyErrorLog(t)
			videoPath := filepath.Join(t.TempDir(), "missing.mp4")
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, "http://127.0.0.1")
			root.SetArgs(tt.args(videoPath))

			if err := root.Execute(); err == nil {
				t.Fatal("Execute() error = nil, want missing video error")
			}
			entries := readDailyErrorLog(t)
			if len(entries) != 1 {
				t.Fatalf("log entries = %d, want 1: %#v", len(entries), entries)
			}
			fields, ok := entries[0]["fields"].(map[string]any)
			if !ok {
				t.Fatalf("fields = %#v, want object", entries[0]["fields"])
			}
			if fields["video_path"] != videoPath {
				t.Fatalf("video_path = %v, want %s", fields["video_path"], videoPath)
			}
		})
	}
}

func assertSingleUploadedVideo(t *testing.T, r *http.Request, wantName string) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Fatalf("upload method = %s, want POST", r.Method)
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("ParseMultipartForm(): %v", err)
	}
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || files[0].Filename != wantName {
		t.Fatalf("uploaded files = %#v, want one %s", files, wantName)
	}
}

func decodeRequestBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var body map[string]any
	if err := sonic.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func assertVideoToolBaseRequest(t *testing.T, body map[string]any, wantMessage string) {
	t.Helper()
	if body["agent_name"] != "pippit_video_part_agent" {
		t.Fatalf("agent_name = %v, want video part agent", body["agent_name"])
	}
	if body["message"] != wantMessage {
		t.Fatalf("message = %v, want %s", body["message"], wantMessage)
	}
	if _, ok := body["asset_ids"]; ok {
		t.Fatalf("asset_ids must be omitted: %#v", body)
	}
}

func videoMiniToolParam(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	videoPart := requiredObject(t, body, "video_part_tool_param")
	for _, unwanted := range []string{"prompt", "videos", "model", "ratio", "resolution", "duration_sec"} {
		if _, ok := videoPart[unwanted]; ok {
			t.Fatalf("video_part_tool_param.%s must be omitted: %#v", unwanted, videoPart)
		}
	}
	return requiredObject(t, videoPart, "mini_tool_param")
}

func assertNestedVideoAsset(t *testing.T, toolParam map[string]any, wantAssetID string) {
	t.Helper()
	video := requiredObject(t, toolParam, "video")
	if video["pippit_asset_id"] != wantAssetID {
		t.Fatalf("pippit_asset_id = %v, want %s", video["pippit_asset_id"], wantAssetID)
	}
	if len(video) != 1 {
		t.Fatalf("video = %#v, want only pippit_asset_id", video)
	}
}

func requiredObject(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", key, parent[key])
	}
	return value
}

func writeTestVideo(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("video-data"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}
