package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
)

func TestNovelSubmitRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"novel", "+submit-run", "--prompt", "write a cyberpunk opening", "--title", "Neon Dawn"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	var got map[string]any
	if err := sonic.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if got["scene"] != "novel" {
		t.Fatalf("scene = %v, want novel", got["scene"])
	}
	if got["status"] != "submitted" {
		t.Fatalf("status = %v, want submitted", got["status"])
	}
	if !strings.HasPrefix(got["thread_id"].(string), "thread_mock_") {
		t.Fatalf("thread_id = %v, want mock thread id", got["thread_id"])
	}
	if !strings.HasPrefix(got["run_id"].(string), "run_mock_") {
		t.Fatalf("run_id = %v, want mock run id", got["run_id"])
	}
}

func TestNovelSubmitRunRequiresPrompt(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"novel", "+submit-run"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--prompt is required") {
		t.Fatalf("error = %q, want prompt validation", err)
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
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"novel", "+get-thread", "--thread-id", "thread_mock_123456"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["scene"] != "novel" {
		t.Fatalf("scene = %v, want novel", got["scene"])
	}
	if got["thread_id"] != "thread_mock_123456" {
		t.Fatalf("thread_id = %v, want thread_mock_123456", got["thread_id"])
	}
	if got["status"] != "active" {
		t.Fatalf("status = %v, want active", got["status"])
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

func decodeJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := sonic.Unmarshal(data, &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, string(data))
	}
	return got
}
