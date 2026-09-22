package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

func TestModelCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != config.GetAvailableModelListPath {
			t.Errorf("path=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ret":"0","data":{"scene":"web_turbo_video_generator","config_key":"key","config":{"models":[{"key":"MiniMax-H3","name":"MiniMax","kind":"video","supported_ratio_list":[0,2,3],"default_ratio":3,"audio_total_limit":0}]}}}`))
	}))
	defer server.Close()
	for i, args := range [][]string{
		{"model", "list"},
		{"model", "search", "MiniMax", "--type", "video"},
		{"model", "describe", "MiniMax-H3"},
		{"model", "MiniMax-H3"},
		{"model", "list", "--refresh"},
	} {
		var stdout, stderr bytes.Buffer
		cfg := config.Load()
		cfg.BaseURL, cfg.AccessKey = server.URL, "model-command-test-key"
		root := newRootCommand(&stdout, &stderr, newRootRunner(cfg))
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var output map[string]json.RawMessage
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			t.Fatal(err)
		}
		if _, exists := output["config_key"]; exists {
			t.Fatal("internal config key must not be exposed")
		}
		if i == 2 || i == 3 {
			if bytes.Contains(output["model"], []byte("supported_ratio_list")) || !bytes.Contains(output["model"], []byte(`"default":"9:16"`)) {
				t.Fatalf("ratio must use CLI strings: %s", stdout.String())
			}
			if !bytes.Contains(output["model"], []byte(`"audio_total_limit":0`)) {
				t.Fatalf("missing config: %s", stdout.String())
			}
		} else if !bytes.Contains(output["models"], []byte("MiniMax-H3")) {
			t.Fatalf("missing model: %s", stdout.String())
		}
		want := 1
		if i == 4 {
			want = 2
		}
		if requests != want {
			t.Fatalf("requests=%d want=%d", requests, want)
		}
	}
}

func TestModelHelpDoesNotRequireLoginOrRequest(t *testing.T) {
	for _, args := range [][]string{{"model", "--help"}, {"generate-video", "--help"}, {"generate-image", "--help"}} {
		var stdout, stderr bytes.Buffer
		root := NewRootCommand(&stdout, &stderr)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "list") {
			t.Fatalf("help must show discovery: %s", stdout.String())
		}
	}
}

func TestImageModelCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["scene"] != "web_image_agent" || body["source_model_key"] != "" || r.URL.Path != config.GetAvailableModelListPath {
			t.Errorf("unexpected request: body=%v path=%s err=%v", body, r.URL.Path, err)
		}
		_, _ = w.Write([]byte(`{"ret":"0","data":{"scene":"web_image_agent","config_key":"image-config","config":{"models":[{"key":"image-model","name":"图片测试模型","kind":"image","is_default":true,"supported_ratio_list":[0,2,6],"parameter_config":{"dimensions":[{"key":"resolution","default_value":"2K","option_list":[{"value":"2K"}]},{"key":"effort","default_value":"low","option_list":[{"value":"low"},{"value":"high"}]}]}}]}}}`))
	}))
	defer server.Close()
	for _, args := range [][]string{
		{"model", "list", "--type", "image"},
		{"model", "search", "图片", "-t", "image"},
		{"model", "describe", "image-model", "--type", "image"},
		{"model", "image-model", "-t", "image"},
	} {
		var stdout, stderr bytes.Buffer
		cfg := config.Load()
		cfg.BaseURL, cfg.AccessKey = server.URL, "image-model-test-key"
		root := newRootCommand(&stdout, &stderr, newRootRunner(cfg))
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(stdout.String(), `"scene": "web_image_agent"`) && !strings.Contains(stdout.String(), `"scene":"web_image_agent"`) {
			t.Fatalf("missing image scene: %s", stdout.String())
		}
		if strings.Contains(stdout.String(), `"is_default"`) || strings.Contains(stdout.String(), `"config_key"`) {
			t.Fatalf("internal fields exposed: %s", stdout.String())
		}
		if args[1] == "describe" || args[1] == "image-model" {
			var output struct {
				Model struct {
					Effort struct{ Options []string }
				}
			}
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil || len(output.Model.Effort.Options) != 2 {
				t.Fatalf("missing effort choices: %s err=%v", stdout.String(), err)
			}
		}
	}
	if requests != 1 {
		t.Fatalf("commands did not share image cache: %d requests", requests)
	}
	var stdout, stderr bytes.Buffer
	cfg := config.Load()
	cfg.BaseURL, cfg.AccessKey = server.URL, "image-model-test-key"
	root := newRootCommand(&stdout, &stderr, newRootRunner(cfg))
	root.SetArgs([]string{"model", "list", "--type", "audio"})
	if err := root.Execute(); err == nil || requests != 1 {
		t.Fatalf("invalid type must fail before request: err=%v requests=%d", err, requests)
	}
}
