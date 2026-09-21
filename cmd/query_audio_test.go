package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestQueryResultRetriesOnlyFailedAudioDownload(t *testing.T) {
	var calls []string
	secondDownloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/biz/v1/skill/get_thread":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if r.Method != http.MethodPost || body["thread_id"] != "thread_123" || body["run_id"] != "run_456" {
				t.Errorf("query must retain the original task: %s %#v", r.Method, body)
			}
			var content []any
			for _, id := range []string{"first", "second"} {
				content = append(content, map[string]any{
					"sub_type": "biz/x_data_audio", "data": map[string]any{"audio": map[string]any{
						"url": serverURL(r) + "/" + id + ".wav", "pippit_asset_id": id,
					}},
				})
			}
			writeAudioQueryFixture(w, map[string]any{"run_id": "run_456", "state": 3, "entry_list": []any{
				map[string]any{"artifact": map[string]any{"content": content}},
			}})
		case "/first.wav", "/second.wav":
			if r.Method != http.MethodGet {
				t.Errorf("download method=%s, want GET", r.Method)
			}
			if r.URL.Path == "/second.wav" {
				secondDownloads++
				if secondDownloads == 1 {
					http.Error(w, "download unavailable", http.StatusForbidden)
					return
				}
			}
			io.WriteString(w, r.URL.Path)
		default:
			t.Errorf("query retry must not upload or submit: %s", r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	first := runAudioQuery(t, server.URL, dir)
	if first["completed"] != false || !strings.Contains(first["error_message"].(string), "下载失败") || first["thread_id"] != "thread_123" || first["run_id"] != "run_456" {
		t.Fatalf("failed download must retain task IDs and report incomplete delivery: %#v", first)
	}
	firstPath := filepath.Join(dir, "first.wav")
	assertFileContent(t, firstPath, "/first.wav")
	beforeData, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeSHA := sha256.Sum256(beforeData)
	secondPath := filepath.Join(dir, "second.wav")
	if _, err := os.Stat(secondPath); !os.IsNotExist(err) {
		t.Fatalf("failed download left a final output: err=%v", err)
	}
	second := runAudioQuery(t, server.URL, dir)
	if second["completed"] != true || second["error_message"] != "" || second["thread_id"] != first["thread_id"] || second["run_id"] != first["run_id"] {
		t.Fatalf("same task should finish delivery on retry: %#v", second)
	}
	audios, ok := second["audios"].([]any)
	if !ok || len(audios) != 2 {
		t.Fatalf("audios=%#v, want both downloaded tracks", second["audios"])
	}
	for i, path := range []string{firstPath, secondPath} {
		if got := audios[i].(map[string]any)["output_path"]; got != path {
			t.Fatalf("audio %d output_path=%v, want %s", i, got, path)
		}
	}
	assertFileContent(t, secondPath, "/second.wav")
	afterData, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(afterData) != beforeSHA || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatal("retry changed the already downloaded first track's SHA or mtime")
	}
	wantCalls := "/api/biz/v1/skill/get_thread,/first.wav,/second.wav,/api/biz/v1/skill/get_thread,/second.wav"
	if got := strings.Join(calls, ","); got != wantCalls {
		t.Fatalf("requests=%s, want %s", got, wantCalls)
	}
}

func TestQueryResultCanRetrySameRunAfterQueryBusinessError(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/biz/v1/skill/get_thread" {
			t.Errorf("retry must not upload or submit: %s", r.URL.Path)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode query: %v", err)
			return
		}
		if body["thread_id"] != "thread_123" || body["run_id"] != "run_456" {
			t.Errorf("query changed task identity: %#v", body)
		}
		requests++
		if requests == 1 {
			io.WriteString(w, `{"ret":"5","errmsg":"服务器繁忙","log_id":"query_log_1"}`)
			return
		}
		writeAudioQueryFixture(w, map[string]any{
			"run_id": "run_456", "state": 4,
			"fail_reason": map[string]any{"message": "下游音频生成失败", "code": 11001},
		})
	}))
	defer server.Close()
	dir := t.TempDir()
	first := runAudioQuery(t, server.URL, dir)
	if first["completed"] != false || first["error_message"] != "查询失败：服务器繁忙 log_id=query_log_1" {
		t.Fatalf("query error must leave Run state unresolved: %#v", first)
	}
	second := runAudioQuery(t, server.URL, dir)
	if second["completed"] != true || second["error_message"] != "下游音频生成失败 (error_code=11001)" {
		t.Fatalf("second query should report observed Run failure: %#v", second)
	}
	if requests != 2 || first["thread_id"] != second["thread_id"] || first["run_id"] != second["run_id"] {
		t.Fatalf("unexpected query sequence: requests=%d first=%#v second=%#v", requests, first, second)
	}
}

func TestQueryResultStructuredBusinessErrorsRequireMatchingTerminalRun(t *testing.T) {
	for _, tc := range []struct {
		name, threadID, runID string
		state                 int
		completed             bool
		data                  any
	}{
		{name: "failed", threadID: "thread_123", runID: "run_456", state: 4, completed: true},
		{name: "canceled", threadID: "thread_123", runID: "run_456", state: 5, completed: true},
		{name: "wrong run", threadID: "thread_123", runID: "other_run", state: 4},
		{name: "wrong thread", threadID: "other_thread", runID: "run_456", state: 4},
		{name: "unknown state", threadID: "thread_123", runID: "run_456", state: 99},
		{name: "working state", threadID: "thread_123", runID: "run_456", state: 2},
		{name: "success state with error ret", threadID: "thread_123", runID: "run_456", state: 3},
		{name: "missing data", data: json.RawMessage(`null`)},
		{name: "missing run", data: map[string]any{"thread": map[string]any{"thread_id": "thread_123"}}},
		{name: "malformed structured data", data: "not a structured thread"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/biz/v1/skill/get_thread" {
					t.Errorf("error result must not download or submit: %s", r.URL.Path)
					return
				}
				data := tc.data
				if data == nil {
					data = map[string]any{"thread": map[string]any{
						"thread_id": tc.threadID, "run_list": []any{map[string]any{
							"run_id": tc.runID, "state": tc.state,
							"fail_reason": map[string]any{"code": 1, "message": "请求的Run已终止"},
						}},
					}}
				}
				json.NewEncoder(w).Encode(map[string]any{"ret": "5", "errmsg": "服务器繁忙", "log_id": "structured_log", "data": data})
			}))
			defer server.Close()
			got := runAudioQuery(t, server.URL, t.TempDir())
			want := "查询失败：服务器繁忙 log_id=structured_log"
			if tc.completed {
				want = "请求的Run已终止 (error_code=1) log_id=structured_log"
			}
			if got["completed"] != tc.completed || got["error_message"] != want || got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
				t.Fatalf("unexpected error outcome: %#v", got)
			}
			for _, kind := range []string{"audios", "images", "videos"} {
				if items, ok := got[kind].([]any); !ok || len(items) != 0 {
					t.Fatalf("%s=%#v, want empty media", kind, got[kind])
				}
			}
		})
	}
}

func runAudioQuery(t *testing.T, baseURL, dir string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, baseURL)
	root.SetArgs([]string{"query-result", "--audio", "--thread-id", "thread_123", "--run-id", "run_456", "--download-dir", dir})
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
