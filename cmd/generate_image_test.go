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

func TestGenerateImage(t *testing.T) {
	var uploaded bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want test bearer token", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/biz/v1/skill/upload_file":
			if r.Method != http.MethodPost {
				t.Fatalf("upload method = %s, want POST", r.Method)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("ParseMultipartForm(): %v", err)
			}
			files := r.MultipartForm.File["file"]
			if len(files) != 1 {
				t.Fatalf("file parts = %d, want 1", len(files))
			}
			if files[0].Filename != "cat.png" {
				t.Fatalf("filename = %q, want cat.png", files[0].Filename)
			}
			uploaded = true
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"pippit_asset_id":"image_asset_1"}}`))
		case "/api/biz/v1/skill/submit_run":
			if !uploaded {
				t.Fatal("submit called before upload")
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			var body map[string]any
			if err := sonic.Unmarshal(data, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["agent_name"] != "pippit_nest_agent" {
				t.Fatalf("agent_name = %v, want nest agent", body["agent_name"])
			}
			if body["message"] != "生成小猫海报" {
				t.Fatalf("message = %v, want prompt", body["message"])
			}
			if _, ok := body["video_part_tool_param"]; ok {
				t.Fatalf("video_part_tool_param should be omitted: %#v", body)
			}
			assetIDs, ok := body["asset_ids"].([]any)
			if !ok || len(assetIDs) != 1 || assetIDs[0] != "image_asset_1" {
				t.Fatalf("asset_ids = %#v, want uploaded asset", body["asset_ids"])
			}
			settings, ok := body["general_agent_settings"].(map[string]any)
			if !ok {
				t.Fatalf("general_agent_settings = %#v, want object", body["general_agent_settings"])
			}
			if settings["image_model"] != "seedream_4.5" {
				t.Fatalf("image_model = %v, want seedream_4.5", settings["image_model"])
			}
			if settings["ratio"] != float64(6) {
				t.Fatalf("ratio = %v, want 6", settings["ratio"])
			}
			if settings["generate_image_count"] != float64(2) {
				t.Fatalf("generate_image_count = %v, want 2", settings["generate_image_count"])
			}
			if _, ok := settings["video_model"]; ok {
				t.Fatalf("video_model should be omitted: %#v", settings)
			}
			_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"run":{"thread_id":"thread_123","run_id":"run_456"},"web_thread_link":"https://xyq.example/thread_123"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cwd := chdirTemp(t)
	image := filepath.Join(cwd, "cat.png")
	if err := os.WriteFile(image, []byte("image-data"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", image, err)
	}

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"generate-image",
		"--prompt", "生成小猫海报",
		"--image", image,
		"--model", "seedream_4.5",
		"--ratio", "6",
		"--generate-image-count", "2",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
		t.Fatalf("output = %#v, want thread and run IDs", got)
	}
}

func TestGenerateImageRequiresModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive request without model")
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"generate-image",
		"--prompt", "x",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want model validation")
	}
	if !strings.Contains(err.Error(), "缺少必填参数 --model") {
		t.Fatalf("error = %q, want model validation", err)
	}
}
