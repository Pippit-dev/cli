package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bytedance/sonic"
)

func TestGenerateVideo(t *testing.T) {
	assetIDs := []string{"image_asset_1", "image_asset_2", "video_asset_1"}
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
			if param["generate_type"] != float64(0) {
				t.Fatalf("generate_type = %v, want 0", param["generate_type"])
			}
			assertAssetRefs(t, param["images"], []string{"image_asset_1", "image_asset_2"})
			assertAssetRefs(t, param["videos"], []string{"video_asset_1"})
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
	for _, path := range []string{image1, image2, video1} {
		if err := os.WriteFile(path, []byte("media-data"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"generate_video",
		"--prompt", "做个小猫视频",
		"--images", image1, image2,
		"--videos", video1,
		"--duration", "5",
		"--ratio", "9:16",
		"--model", "seedance2.0_vision",
		"--resolution", "720p",
		"--generate-type", "0",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
		t.Fatalf("output = %#v, want thread and run IDs", got)
	}
	assertStringSlice(t, got["image_asset_ids"], []string{"image_asset_1", "image_asset_2"})
	assertStringSlice(t, got["video_asset_ids"], []string{"video_asset_1"})
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
		"generate_video",
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

func assertAssetRefs(t *testing.T, got any, want []string) {
	t.Helper()
	items, ok := got.([]any)
	if !ok || len(items) != len(want) {
		t.Fatalf("asset refs = %#v, want %v", got, want)
	}
	for i, item := range items {
		ref, ok := item.(map[string]any)
		if !ok || ref["asset_id"] != want[i] {
			t.Fatalf("asset refs[%d] = %#v, want %s", i, item, want[i])
		}
	}
}

func assertStringSlice(t *testing.T, got any, want []string) {
	t.Helper()
	items, ok := got.([]any)
	if !ok || len(items) != len(want) {
		t.Fatalf("slice = %#v, want %v", got, want)
	}
	for i, item := range items {
		if item != want[i] {
			t.Fatalf("slice[%d] = %v, want %s", i, item, want[i])
		}
	}
}
