package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This matrix follows the legacy skill query endpoint, including its middleware.
// Business error envelopes deliberately omit Run, as the real handler does.
func TestQueryResultResponseBranches(t *testing.T) {
	tests := []struct {
		name, ret, message, status string
		httpStatus                 int
	}{
		{"empty thread id", "2", "thread id不能为空", "unknown", 200},
		{"invalid version", "2", "version参数无效", "unknown", 200},
		{"empty run id", "2", "run id不能为空", "unknown", 200},
		{"invalid AK owner info", "2", "无法根据此Ak找到有效用户信息", "unknown", 200},
		{"invalid OAuth user info", "2", "无法获取有效用户信息", "unknown", 200},
		{"RPC or DB error", "5", "获取会话信息失败", "unknown", 200},
		{"no runs", "1", "会话中缺少有效的Run", "unknown", 200},
		{"run not found", "2", "会话中未找到RunID为run_456的Run", "unknown", 200},
		{"run copy failed", "1", "解析Run信息失败", "unknown", 200},
		{"failed without reason", "5", "创作失败", "failed", 200},
		{"failed with reason", "5", "创作失败：视频可能不合规，请修改后重试", "failed", 200},
		{"failed with empty reason", "5", "创作失败：", "failed", 200},
		{"canceled", "5", "创作已取消", "canceled", 200},
		{"bad bearer or AK", "1015", "invalid access key", "unknown", 401},
		{"bad owner", "1015", "invalid access key owner", "unknown", 401},
		{"bad team id", "1015", "invalid team id", "unknown", 401},
		{"team login missing", "1015", "login required for team access", "unknown", 401},
		{"permission RPC failure", "1015", "team permission check failed", "unknown", 401},
		{"permission RPC rejection", "1015", "team permission denied", "unknown", 401},
		{"team denied", "16019", "team permission denied", "unknown", 200},
		{"member inactive", "16020", "team member is inactive", "unknown", 200},
		{"role denied", "16021", "team role is not allowed", "unknown", 200},
		{"team missing", "16022", "team not found", "unknown", 200},
		{"team unavailable", "16023", "team is unavailable", "unknown", 200},
		{"subscription inactive", "16024", "team subscription is inactive", "unknown", 200},
		{"member limit", "16025", "team member limit exceeded", "unknown", 200},
		{"product identity invalid", "2", "missing or unsupported business app id", "unknown", 400},
		{"product identity unavailable", "5", "business app identity unavailable", "unknown", 503},
		{"request binding error", "-3", "bad request", "unknown", 200},
		{"panic", "-2", "system panic", "unknown", 500},
		{"unknown future ret", "987654", "new server error", "unknown", 200},
		{"wrong ret with failure phrase", "2", "创作失败：原因", "unknown", 200},
		{"wrong ret with cancellation phrase", "1", "创作已取消", "unknown", 200},
		{"unrecognized ret with failure phrase", "987654", "创作失败", "unknown", 200},
		{"zero ret without run", "0", "创作失败", "unknown", 200},
		{"contains failure word", "5", "查询创作失败记录失败", "unknown", 200},
		{"only moderation wording", "5", "视频可能不合规", "unknown", 200},
		{"wrong failure delimiter", "5", "创作失败: reason", "unknown", 200},
		{"cancellation suffix", "5", "创作已取消查询失败", "unknown", 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"ret": tt.ret, "errmsg": tt.message, "log_id": "branch-log", "data": nil})
			if err != nil {
				t.Fatal(err)
			}
			got := executeQueryResponse(t, tt.httpStatus, string(body))
			completed := tt.status == "failed" || tt.status == "canceled"
			if got["status"] != tt.status || got["completed"] != completed {
				t.Fatalf("got %#v, want %s completed=%t", got, tt.status, completed)
			}
			message, _ := got["error_message"].(string)
			if tt.ret != "0" && (!strings.Contains(message, tt.message) || !strings.Contains(message, "branch-log")) {
				t.Fatalf("original error/LogID lost: %q", message)
			}
			if message == "" {
				t.Fatalf("missing error: %#v", got)
			}
		})
	}
}

func executeQueryResponse(t *testing.T, status int, body string) map[string]any {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/biz/v1/skill/get_thread" {
			t.Errorf("unexpected download/request: %s", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	return executeQueryURL(t, server.URL, t.TempDir())
}

func executeQueryURL(t *testing.T, url, dir string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, url)
	root.SetArgs([]string{"query-result", "--thread-id", "thread_123", "--run-id", "run_456", "--download-dir", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v, stderr=%s", err, stderr.String())
	}
	got := decodeJSON(t, stdout.Bytes())
	if got["thread_id"] != "thread_123" || got["run_id"] != "run_456" {
		t.Fatalf("requested IDs lost: %#v", got)
	}
	for _, key := range []string{"videos", "images"} {
		if _, ok := got[key].([]any); !ok {
			t.Fatalf("%s is not an array: %#v", key, got)
		}
	}
	return got
}

func TestQueryResultRecordedFailureResponse(t *testing.T) {
	// Recorded production response; only diagnostic identifiers are sanitized.
	data, err := os.ReadFile("testdata/query-result/recorded-failure.json")
	if err != nil {
		t.Fatal(err)
	}
	got := executeQueryResponse(t, 200, string(data))
	if got["status"] != "failed" || got["completed"] != true || got["error_message"] != "创作失败：视频可能不合规，请修改后重试 log_id=recorded-failure-log" {
		t.Fatalf("recorded failure was not recognized: %#v", got)
	}
}

func TestQueryResultRunPrecedesEveryBusinessError(t *testing.T) {
	states := []struct {
		state     int
		status    string
		completed bool
	}{
		{0, "unknown", false}, {1, "running", false}, {2, "running", false},
		{3, "succeeded", true}, {4, "failed", true}, {5, "canceled", true},
		{6, "input_required", false}, {7, "running", false}, {8, "running", false},
		{9, "interrupted", true}, {99, "unknown", false},
	}
	// These are compatibility envelopes, not assertions that the server currently
	// returns a structured Run for every error. The recorded fixture covers that.
	for _, ret := range []string{"0", "1", "2", "5", "1015", "16019", "16020", "16021", "16022", "16023", "16024", "16025", "-2", "-3", "987654"} {
		for _, state := range states {
			t.Run(fmt.Sprintf("ret=%s/state=%d", ret, state.state), func(t *testing.T) {
				clearDailyErrorLog(t)
				got := executeQueryResponse(t, 200, fmt.Sprintf(`{"ret":%q,"errmsg":"创作失败：envelope reason","log_id":"matrix-log","data":{"thread":{"run_list":[{"run_id":"other","state":4},{"run_id":"run_456","state":%d,"fail_reason":{"message":"Run reason","code":12023}}]}}}`, ret, state.state))
				if got["status"] != state.status || got["completed"] != state.completed {
					t.Fatalf("got %#v, want %#v", got, state)
				}
				if state.state == 4 && got["error_message"] != "Run reason (error_code=12023)" {
					t.Fatalf("lost run reason: %#v", got)
				}
				if state.state != 3 && state.state != 4 && got["error_message"] != "" {
					t.Fatalf("envelope overwrote Run: %#v", got)
				}
				if ret != "0" {
					logs, err := json.Marshal(readDailyErrorLog(t))
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(logs), "ret="+ret) || !strings.Contains(string(logs), "matrix-log") {
						t.Fatalf("missing diagnostics: %s", logs)
					}
				}
			})
		}
	}
}

func TestQueryResultUnusableResponses(t *testing.T) {
	for _, body := range []string{
		``, `<html>gateway error</html>`, `{"ret":`,
		`{}`, `{"ret":"0"}`, `{"ret":"0","data":null}`,
		`{"ret":"0","data":{}}`, `{"ret":"0","data":{"readable_text":"失败"}}`,
		`{"ret":"0","data":{"thread":null}}`,
		`{"ret":"0","data":{"thread":{"run_list":[]}}}`,
		`{"ret":"0","data":{"thread":{"run_list":"invalid"}}}`,
		`{"ret":"0","data":{"thread":{"run_list":[{"run_id":"other","state":4}]}}}`,
		`{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run_456","state":"bad"}]}}}`,
		`{"ret":"","errmsg":"创作失败","data":null}`,
		`{"ret":"5","errmsg":"","data":null}`,
	} {
		t.Run(body, func(t *testing.T) {
			got := executeQueryResponse(t, 200, body)
			if got["status"] != "unknown" || got["completed"] != false || got["error_message"] == "" {
				t.Fatalf("got %#v", got)
			}
		})
	}
	for _, code := range []int{400, 401, 403, 429, 500, 502, 503, 504} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			got := executeQueryResponse(t, code, "gateway error")
			if got["status"] != "unknown" || got["completed"] != false || !strings.Contains(got["error_message"].(string), fmt.Sprint(code)) {
				t.Fatalf("got %#v", got)
			}
		})
	}
	t.Run("connection refused", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		server.Close()
		got := executeQueryURL(t, server.URL, t.TempDir())
		if got["status"] != "unknown" || got["completed"] != false || got["error_message"] == "" {
			t.Fatalf("got %#v", got)
		}
	})
}

func TestQueryResultRetainsPartialDownloads(t *testing.T) {
	for _, mode := range []string{"second video fails", "first image fails", "second image fails", "cached video then image fails"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/biz/v1/skill/get_thread" {
					content := []any{map[string]any{"sub_type": "biz/x_data_video", "data": map[string]any{"video": map[string]any{"download_url": serverURL(r) + "/video", "vid": "first"}}}}
					badVideo := map[string]any{"sub_type": "biz/x_data_video", "data": map[string]any{"video": map[string]any{"download_url": "invalid-scheme://broken"}}}
					badImage := map[string]any{"sub_type": "biz/x_data_image", "data": map[string]any{"image": map[string]any{"url": "invalid-scheme://broken"}}}
					if mode == "second video fails" {
						content = append(content, badVideo)
					} else {
						if mode == "second image fails" {
							content = append(content, map[string]any{"sub_type": "biz/x_data_image", "data": map[string]any{"image": map[string]any{"url": serverURL(r) + "/image", "asset_id": "first-image"}}})
						}
						content = append(content, badImage)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"ret": "0", "data": map[string]any{"thread": map[string]any{"run_list": []any{map[string]any{"run_id": "run_456", "state": 3, "entry_list": []any{map[string]any{"artifact": map[string]any{"content": content}}}}}}}})
					return
				}
				_, _ = w.Write([]byte("media-data"))
			}))
			defer server.Close()
			dir := t.TempDir()
			if mode == "cached video then image fails" {
				if err := os.WriteFile(filepath.Join(dir, "first.mp4"), []byte("media-data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got := executeQueryURL(t, server.URL, dir)
			if got["status"] != "succeeded" || got["completed"] != true || !strings.Contains(got["error_message"].(string), "下载失败") {
				t.Fatalf("got %#v", got)
			}
			videos := got["videos"].([]any)
			images := got["images"].([]any)
			wantImages := 0
			if mode == "second image fails" {
				wantImages = 1
			}
			if len(videos) != 1 || len(images) != wantImages {
				t.Fatalf("partial files lost: %#v", got)
			}
			for _, file := range append(videos, images...) {
				assertFileContent(t, file.(map[string]any)["output_path"].(string), "media-data")
			}
		})
	}
}

func TestQueryResultTerminalEnvelopeWithoutUsableRun(t *testing.T) {
	for _, data := range []string{`null`, `{}`, `{"thread":null}`, `{"thread":{"run_list":[]}}`, `{"thread":{"run_list":[{"run_id":"other","state":3}]}}`, `{"thread":{"run_list":"bad"}}`} {
		t.Run(data, func(t *testing.T) {
			got := executeQueryResponse(t, 200, `{"ret":"5","errmsg":"创作失败：reason","data":`+data+`}`)
			if got["status"] != "failed" || got["completed"] != true || got["error_message"] != "创作失败：reason" {
				t.Fatalf("got %#v", got)
			}
		})
	}
}
