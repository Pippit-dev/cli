package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
)

func TestQueryResultDefaultPreservesLegacyStatesAndOutput(t *testing.T) {
	for _, tc := range []struct {
		name, response, message string
		completed               bool
	}{
		{"business error without data", `{"ret":"5","errmsg":"legacy failure","log_id":"query_log"}`, "legacy failure log_id=query_log", true},
		{"business error with audio terminal data", `{"ret":"5","errmsg":"legacy failure","log_id":"query_log","data":{"thread":{"thread_id":"thread_123","run_list":[{"run_id":"run_456","state":4,"fail_reason":{"message":"audio reason"}}]}}}`, "legacy failure log_id=query_log", true},
		{"canceled", `{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run_456","state":5,"fail_reason":{"message":"canceled reason"}}]}}}`, "", false},
		{"unknown state", `{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run_456","state":99}]}}}`, "", false},
		{"legacy artifact error wins", `{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run_456","state":4,"fail_reason":{"message":"audio reason"},"entry_list":[{"artifact":{"content":[{"data":{"error_message":"legacy reason","error_code":"7"}}]}}]}]}}}`, "legacy reason (error_code=7)", true},
		{"audio alone is not a legacy download", `{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run_456","state":3,"entry_list":[{"artifact":{"content":[{"sub_type":"biz/x_data_audio","data":{"audio":{"url":"https://example.com/audio.wav"}}}]}}]}]}}}`, "下载失败：未找到可下载的产物", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/biz/v1/skill/get_thread" || !reflect.DeepEqual(body, map[string]any{"thread_id": "thread_123", "run_id": "run_456"}) {
					t.Errorf("legacy query request changed: %s %s %#v", r.Method, r.URL.Path, body)
				}
				io.WriteString(w, tc.response)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			root.SetArgs([]string{"query-result", "--thread-id", "thread_123", "--run-id", "run_456", "--download-dir", t.TempDir()})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"completed": tc.completed, "thread_id": "thread_123", "run_id": "run_456", "error_message": tc.message, "videos": []any{}, "images": []any{}}
			if got := decodeJSON(t, stdout.Bytes()); !reflect.DeepEqual(got, want) {
				t.Fatalf("legacy output=%#v, want %#v", got, want)
			}
			if requests != 1 {
				t.Fatalf("requests=%d, want one query and no downloads", requests)
			}
		})
	}
}

func TestQueryResultDefaultIgnoresAudioFieldsInLegacyMedia(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/biz/v1/skill/get_thread" {
			// These fields were unknown to the legacy decoder and must remain ignored.
			writeAudioQueryFixture(w, map[string]any{"run_id": "run_456", "state": 3, "fail_reason": "opaque legacy value", "entry_list": []any{
				map[string]any{"artifact": map[string]any{"content": []any{
					map[string]any{"sub_type": "biz/x_data_image", "data": map[string]any{"image": map[string]any{"url": serverURL(r) + "/image.png", "asset_id": "image"}, "audio": "opaque legacy value"}},
					map[string]any{"sub_type": "biz/x_data_video", "data": map[string]any{"video": map[string]any{"download_url": serverURL(r) + "/video.mp4", "vid": "video"}}},
				}}},
			}})
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("download method=%s, want GET", r.Method)
		}
		io.WriteString(w, r.URL.Path)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	dir := t.TempDir()
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{"query-result", "--thread-id", "thread_123", "--run-id", "run_456", "--download-dir", dir})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := decodeJSON(t, stdout.Bytes())
	if got["completed"] != true || got["error_message"] != "" || len(got) != 6 {
		t.Fatalf("legacy media output changed: %#v", got)
	}
	for _, kind := range []string{"images", "videos"} {
		if items, ok := got[kind].([]any); !ok || len(items) != 1 {
			t.Fatalf("%s=%#v, want one legacy result", kind, got[kind])
		}
	}
	assertFileContent(t, filepath.Join(dir, "image.png"), "/image.png")
	assertFileContent(t, filepath.Join(dir, "video.mp4"), "/video.mp4")
	if !reflect.DeepEqual(paths, []string{"/api/biz/v1/skill/get_thread", "/video.mp4", "/image.png"}) {
		t.Fatalf("legacy query/download order changed: %v", paths)
	}
}
