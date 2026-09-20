package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueryResultDownloadsAudioAndMixedMedia(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		name := "audio only"
		if mixed {
			name = "mixed media and string data"
		}
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/biz/v1/skill/get_thread" {
					if r.Method != http.MethodGet {
						t.Errorf("download method=%s", r.Method)
					}
					io.WriteString(w, r.URL.Path)
					return
				}
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("unexpected query method/auth")
				}
				part := func(kind string, value map[string]any) map[string]any {
					var data any = map[string]any{kind: value}
					if mixed {
						encoded, _ := json.Marshal(data)
						data = string(encoded)
					}
					return map[string]any{"sub_type": "biz/x_data_" + kind, "data": data}
				}
				content := []any{part("audio", map[string]any{
					"url": serverURL(r) + "/audio.wav?signature=secret", "name": "warm voice", "pippit_asset_id": "audio_123",
					"metadata": map[string]any{"format": "audio/wav", "duration": 3.25},
				})}
				if mixed {
					content = append(content,
						part("image", map[string]any{"url": serverURL(r) + "/poster.png", "asset_id": "poster", "metadata": map[string]any{"format": "png"}}),
						part("video", map[string]any{"download_url": serverURL(r) + "/clip.mp4", "vid": "clip"}),
					)
				}
				writeAudioQueryFixture(w, map[string]any{"run_id": "run_456", "state": 3, "entry_list": []any{map[string]any{"artifact": map[string]any{"content": content}}}})
			}))
			defer server.Close()
			dir := t.TempDir()
			got := runAudioQuery(t, server.URL, dir)
			if got["completed"] != true || got["error_message"] != "" || got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
				t.Fatalf("unexpected query result: %#v", got)
			}
			audios, ok := got["audios"].([]any)
			if !ok || len(audios) != 1 {
				t.Fatalf("audios=%#v", got["audios"])
			}
			audio := audios[0].(map[string]any)
			if audio["download_url"] != server.URL+"/audio.wav?signature=secret" || audio["name"] != "warm voice" || audio["pippit_asset_id"] != "audio_123" || audio["duration"] != 3.25 || audio["output_path"] != filepath.Join(dir, "audio_123.wav") {
				t.Fatalf("unexpected audio: %#v", audio)
			}
			assertFileContent(t, audio["output_path"].(string), "/audio.wav")
			for _, kind := range []string{"images", "videos"} {
				media, ok := got[kind].([]any)
				want := 0
				if mixed {
					want = 1
				}
				if !ok || len(media) != want {
					t.Fatalf("%s=%#v", kind, got[kind])
				}
			}
			if mixed {
				assertFileContent(t, filepath.Join(dir, "poster.png"), "/poster.png")
				assertFileContent(t, filepath.Join(dir, "clip.mp4"), "/clip.mp4")
			}
		})
	}
}

func TestQueryResultAudioFailureAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, reason, fallback, want string
		state                        int
		completed                    bool
	}{
		{name: "failed reason", state: 4, reason: "音频生成失败", want: "音频生成失败 (error_code=11001)", completed: true},
		{name: "fallback", state: 4, fallback: "积分不足", want: "积分不足 (error_code=11001)", completed: true},
		{name: "canceled", state: 5, want: "Run 已取消", completed: true},
		{name: "canceled reason", state: 5, reason: "用户取消生成", want: "用户取消生成 (error_code=11001)", completed: true},
		{name: "generating", state: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/biz/v1/skill/get_thread" {
					t.Errorf("non-success run requested a download: %s", r.URL.Path)
				}
				writeAudioQueryFixture(w, map[string]any{
					"run_id": "run_456", "state": tc.state,
					"fail_reason": map[string]any{"message": tc.reason, "fallback_message": tc.fallback, "code": 11001},
				})
			}))
			defer server.Close()
			got := runAudioQuery(t, server.URL, t.TempDir())
			if got["completed"] != tc.completed || got["error_message"] != tc.want {
				t.Fatalf("query result=%#v, want completed=%v, error=%s", got, tc.completed, tc.want)
			}
			if audios, ok := got["audios"].([]any); !ok || len(audios) != 0 {
				t.Fatalf("audios=%#v", got["audios"])
			}
		})
	}
}

func TestQueryResultAudioDownloadErrorsKeepTaskIDs(t *testing.T) {
	for _, tc := range []struct{ name, subtype, path, want string }{
		{"missing url", "biz/x_data_audio", "", "音频产物 url 为空"},
		{"wrong subtype", "text/plain", "/audio.wav", "未找到可下载的产物"},
		{"HTTP failure", "biz/x_data_audio", "/audio.wav", "下载失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/biz/v1/skill/get_thread" {
					http.Error(w, "unavailable", http.StatusForbidden)
					return
				}
				audioURL := ""
				if tc.path != "" {
					audioURL = serverURL(r) + tc.path
				}
				writeAudioQueryFixture(w, map[string]any{"run_id": "run_456", "state": 3, "entry_list": []any{map[string]any{
					"artifact": map[string]any{"content": []any{map[string]any{
						"sub_type": tc.subtype, "data": map[string]any{"audio": map[string]any{"url": audioURL}},
					}}},
				}}})
			}))
			defer server.Close()
			got := runAudioQuery(t, server.URL, t.TempDir())
			if !strings.Contains(got["error_message"].(string), tc.want) || got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
				t.Fatalf("unexpected result: %#v", got)
			}
			if audios, ok := got["audios"].([]any); !ok || len(audios) != 0 {
				t.Fatalf("audios=%#v", got["audios"])
			}
		})
	}
}

func runAudioQuery(t *testing.T, baseURL, dir string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, baseURL)
	root.SetArgs([]string{"query-result", "--thread-id", "thread_123", "--run-id", "run_456", "--download-dir", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("query Execute: %v", err)
	}
	return decodeJSON(t, stdout.Bytes())
}

func writeAudioQueryFixture(w http.ResponseWriter, run map[string]any) {
	json.NewEncoder(w).Encode(map[string]any{"ret": "0", "data": map[string]any{
		"thread": map[string]any{"thread_id": "thread_123", "run_list": []any{run}},
	}})
}
