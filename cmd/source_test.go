package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

func TestSkillSubmitSourcePreservesCreativeRequest(t *testing.T) {
	for _, key := range []string{"PIPPIT_CLI_SOURCE", "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CLAUDECODE", "CURSOR_AGENT", "GEMINI_CLI"} {
		t.Setenv(key, "")
	}

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	for _, name := range []string{"submit-run", "generate-image", "generate-video", "video-super-resolution", "erase-video-subtitle", "short-drama"} {
		t.Run(name, func(t *testing.T) {
			video := filepath.Join(t.TempDir(), "input.mp4")
			if err := os.WriteFile(video, []byte("test-video"), 0600); err != nil {
				t.Fatal(err)
			}
			args := map[string][]string{
				"submit-run":             {"submit-run", "--message", "  保留原文\nworkbuddy 只是内容  ", "--thread-id", "skill_existing", "--asset-ids", "asset_1"},
				"generate-image":         {"generate-image", "--prompt", "cat", "--model", "Seedream 5.0 Pro", "--ratio", "6", "--resolution", "4K"},
				"generate-video":         {"generate-video", "--prompt", "cat", "--model", "Seedance_2.0_mini", "--duration", "4", "--ratio", "16:9"},
				"video-super-resolution": {"video-super-resolution", "--video", video, "--output-resolution", "1080p"},
				"erase-video-subtitle":   {"erase-video-subtitle", "--video", video},
				"short-drama":            {"short-drama", "+submit-run", "--message", "剧本", "--thread-id", "skill_existing", "--asset-ids", "asset_1"},
			}[name]
			var received map[string]any
			var query string
			response := `{"ret":"0","data":{"run":{"thread_id":"thread_1","run_id":"run_1"},"web_thread_link":"https://xyq.example/thread_1"}}`
			submitCount := 0
			uploadFailure := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case config.GetAvailableModelListPath:
					_, _ = w.Write([]byte(`{"ret":"0","data":{"scene":"web_image_agent","config_key":"image-key","config":{"models":[{"key":"seedream_5.0_pro","name":"Seedream 5.0 Pro","kind":"image"}]}}}`))
				case config.SubmitRunPath:
					submitCount++
					if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
						t.Error("method or authorization changed")
					}
					if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
						t.Error(err)
					}
					query = r.URL.RawQuery
					_, _ = w.Write([]byte(response))
				case "/api/biz/v1/skill/upload_file":
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
					}
					if r.FormValue("platform") != "" || r.FormValue("source") != "" || r.URL.Query().Has("source") || r.URL.Query().Has("platform") {
						t.Error("host attribution leaked into upload")
					}
					if uploadFailure {
						_, _ = w.Write([]byte(`{"ret":"1","errmsg":"upload rejected"}`))
						return
					}
					_, _ = w.Write([]byte(`{"ret":"0","data":{"pippit_asset_id":"video_1"}}`))
				default:
					t.Errorf("unexpected request path: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			execute := func(sourceFlags ...string) string {
				t.Helper()
				received = nil
				var stdout, stderr bytes.Buffer
				root := newTestRootCommand(t, &stdout, &stderr, server.URL)
				commandArgs := append([]string(nil), args...)
				commandArgs = append(commandArgs, sourceFlags...)
				root.SetArgs(commandArgs)
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				if received == nil {
					t.Fatal("no skill submission received")
				}
				return stdout.String()
			}
			wantOutput := execute()
			if _, exists := received["platform"]; exists {
				t.Fatal("missing source must omit platform")
			}
			wantBody, wantQuery := received, query
			for i, tc := range []struct {
				flags []string
				want  string
			}{
				{[]string{"--source", "workbuddy"}, "workbuddy"},
				{[]string{"--source", " doubao_office "}, "doubao_office"},
				{[]string{"--source", "codex"}, "codex"},
				{[]string{"--source", "新的宿主"}, "新的宿主"},
				{[]string{"--source", ""}, ""},
				{[]string{"--source", " \t "}, ""},
				{[]string{"--source=workbuddy"}, "workbuddy"},
				{[]string{"--source="}, ""},
				{[]string{"--source=codex", "--source=workbuddy"}, "workbuddy"},
				{[]string{"--source=codex", "--source="}, ""},
				{[]string{"--source", "\u3000codex\u00a0"}, "codex"},
				{[]string{"--source", "host\"&=?/\\测试\nnext"}, "host\"&=?/\\测试\nnext"},
			} {
				t.Run(fmt.Sprintf("source_%02d", i), func(t *testing.T) {
					if got := execute(tc.flags...); got != wantOutput {
						t.Errorf("source changed output: %s", got)
					}
					value, exists := received["platform"]
					if (tc.want == "" && exists) || (tc.want != "" && value != tc.want) {
						t.Errorf("platform = %#v (present=%v), want=%q", value, exists, tc.want)
					}
					delete(received, "platform")
					if !reflect.DeepEqual(received, wantBody) || query != wantQuery {
						t.Errorf("source changed creative request or ROI query: %#v / %s", received, query)
					}
				})
			}
			t.Run("host_environment", func(t *testing.T) {
				t.Setenv("CODEX_THREAD_ID", "session-must-not-be-reported")
				execute()
				if received["platform"] != "codex" {
					t.Errorf("runtime host not detected: %#v", received["platform"])
				}
				t.Setenv("PIPPIT_CLI_SOURCE", " workbuddy ")
				execute()
				if received["platform"] != "workbuddy" {
					t.Error("explicit environment must win")
				}
				execute("--source", "doubao_office")
				if received["platform"] != "doubao_office" {
					t.Error("explicit flag must win")
				}
				execute("--source", "")
				if _, exists := received["platform"]; exists {
					t.Error("explicit empty must suppress attribution")
				}
			})
			// A fresh invocation without source must not reuse previous attribution.
			execute()
			if _, exists := received["platform"]; exists {
				t.Error("source leaked into next invocation")
			}
			for _, failure := range []struct{ name, body string }{
				{"business_error", `{"ret":"1","errmsg":"rejected"}`},
				{"invalid_json", `invalid-json`},
			} {
				t.Run(failure.name, func(t *testing.T) {
					response = failure.body
					submitCount = 0
					var stdout, stderr bytes.Buffer
					root := newTestRootCommand(t, &stdout, &stderr, server.URL)
					root.SetArgs(append(append([]string(nil), args...), "--source=codex"))
					if err := root.Execute(); err == nil {
						t.Fatal("source swallowed submit error")
					}
					if submitCount != 1 {
						t.Errorf("submission count = %d, want 1", submitCount)
					}
					if received["platform"] != "codex" {
						t.Error("failed submission lost source")
					}
				})
			}
			if name == "video-super-resolution" || name == "erase-video-subtitle" {
				t.Run("upload_failure", func(t *testing.T) {
					uploadFailure = true
					submitCount = 0
					var stdout, stderr bytes.Buffer
					root := newTestRootCommand(t, &stdout, &stderr, server.URL)
					root.SetArgs(append(append([]string(nil), args...), "--source=workbuddy"))
					if err := root.Execute(); err == nil {
						t.Fatal("source swallowed upload error")
					}
					if submitCount != 0 {
						t.Errorf("submitted %d times after failed upload", submitCount)
					}
				})
			}
		})
	}
}

func TestSourceHelpWithoutCredentials(t *testing.T) {
	for _, args := range [][]string{
		{"submit-run"}, {"generate-image"}, {"generate-video"},
		{"video-super-resolution"}, {"erase-video-subtitle"}, {"short-drama", "+submit-run"},
	} {
		var stdout, stderr bytes.Buffer
		root := newRootCommand(&stdout, &stderr, nil)
		root.SetArgs(append(args, "--help"))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "--source") {
			t.Errorf("help for %v missing --source", args)
		}
	}
}
