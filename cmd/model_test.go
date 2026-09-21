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
	for _, args := range [][]string{{"model", "--help"}, {"generate-video", "--help"}} {
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
