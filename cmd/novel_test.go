package cmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/bytedance/sonic"
	"github.com/spf13/cobra"
)

func TestNovelSubmitRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/biz/v1/skill/submit_run" {
			t.Fatalf("path = %s, want submit_run path", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want test bearer token", r.Header.Get("Authorization"))
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body map[string]any
		if err := sonic.Unmarshal(data, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["message"] != "write a cyberpunk opening" {
			t.Fatalf("message = %v, want submitted message", body["message"])
		}
		if body["thread_id"] != "thread_123" {
			t.Fatalf("thread_id = %v, want thread_123", body["thread_id"])
		}
		assetIDs, ok := body["asset_ids"].([]any)
		if !ok || len(assetIDs) != 2 || assetIDs[0] != "asset_1" || assetIDs[1] != "asset_2" {
			t.Fatalf("asset_ids = %#v, want two asset ids", body["asset_ids"])
		}
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"run":{"thread_id":"thread_123","run_id":"run_456"},"web_thread_link":"https://xyq.example/thread_123"}}`))
	}))
	defer server.Close()
	t.Setenv("PIPPIT_CLI_BASE_URL", server.URL)

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr)
	root.SetArgs([]string{
		"novel", "+submit-run",
		"--message", "write a cyberpunk opening",
		"--thread-id", "thread_123",
		"--asset-ids", "asset_1",
		"--asset-ids", "asset_2",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["thread_id"] != "thread_123" {
		t.Fatalf("thread_id = %v, want thread_123", got["thread_id"])
	}
	if got["run_id"] != "run_456" {
		t.Fatalf("run_id = %v, want run_456", got["run_id"])
	}
	if got["web_thread_link"] != "https://xyq.example/thread_123" {
		t.Fatalf("web_thread_link = %v, want returned link", got["web_thread_link"])
	}
}

func TestNovelSubmitRunRequiresMessage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"novel", "+submit-run"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--message is required") {
		t.Fatalf("error = %q, want message validation", err)
	}
}

func TestNovelUploadFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"novel", "+upload-file", "--path", "/tmp/story.md"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["scene"] != "novel" {
		t.Fatalf("scene = %v, want novel", got["scene"])
	}
	if got["status"] != "uploaded" {
		t.Fatalf("status = %v, want uploaded", got["status"])
	}
	if !strings.HasPrefix(got["file_id"].(string), "file_mock_") {
		t.Fatalf("file_id = %v, want mock file id", got["file_id"])
	}
}

func TestNovelUploadFileRequiresPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"novel", "+upload-file"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--path is required") {
		t.Fatalf("error = %q, want path validation", err)
	}
}

func TestNovelGetThread(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/biz/v1/skill/get_thread" {
			t.Fatalf("path = %s, want get_thread path", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want test bearer token", r.Header.Get("Authorization"))
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body map[string]any
		if err := sonic.Unmarshal(data, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["thread_id"] != "thread_123" {
			t.Fatalf("thread_id = %v, want thread_123", body["thread_id"])
		}
		if body["run_id"] != "run_456" {
			t.Fatalf("run_id = %v, want run_456", body["run_id"])
		}
		if body["after_seq"] != float64(7) {
			t.Fatalf("after_seq = %v, want 7", body["after_seq"])
		}
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"thread":{"run_list":[{"state":3,"entry_list":[{"message":{"message_id":"msg_1","role":"assistant","content":[{"text":"hello"}],"client_tool_calls":[{"name":"tool_call"}]}},{"artifact":{"artifact_id":"artifact_1","role":"assistant","content":[{"type":"image"}]}}]}]}}}`))
	}))
	defer server.Close()
	t.Setenv("PIPPIT_CLI_BASE_URL", server.URL)

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr)
	root.SetArgs([]string{
		"novel", "+get-thread",
		"--thread-id", "thread_123",
		"--run-id", "run_456",
		"--after-seq", "7",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	messages, ok := got["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v, want two entries", got["messages"])
	}
	first, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("first message = %#v, want object", messages[0])
	}
	if first["id"] != "msg_1" {
		t.Fatalf("first id = %v, want msg_1", first["id"])
	}
	content, ok := first["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("first content = %#v, want message content plus tool call", first["content"])
	}
}

func TestNovelGetThreadRequiresThreadID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"novel", "+get-thread"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--thread-id is required") {
		t.Fatalf("error = %q, want thread-id validation", err)
	}
}

type testAuthorizer struct {
	t         *testing.T
	refreshed bool
}

func (a *testAuthorizer) Refresh(ctx context.Context, ensureTTL time.Duration) error {
	a.t.Helper()
	if err := ctx.Err(); err != nil {
		return err
	}
	if ensureTTL != config.DefaultAuthTTL {
		a.t.Fatalf("ensureTTL = %s, want %s", ensureTTL, config.DefaultAuthTTL)
	}
	a.refreshed = true
	return nil
}

func (a *testAuthorizer) Inject(ctx context.Context, req *http.Request) error {
	a.t.Helper()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.refreshed {
		a.t.Fatal("Inject called before Refresh")
	}
	req.Header.Set("Authorization", "Bearer test-token")
	return nil
}

func newTestRootCommand(t *testing.T, stdout, stderr io.Writer) *cobra.Command {
	t.Helper()
	runner := common.NewRunner(config.Load())
	runner.Authorizer = &testAuthorizer{t: t}
	return newRootCommand(stdout, stderr, runner)
}

func decodeJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := sonic.Unmarshal(data, &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, string(data))
	}
	return got
}
