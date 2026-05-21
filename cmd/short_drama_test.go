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

	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/bytedance/sonic"
	"github.com/spf13/cobra"
)

func TestShortDramaSubmitRun(t *testing.T) {
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
		if body["agent_name"] != "pippit_nest_short_drama_agent" {
			t.Fatalf("agent_name = %v, want pippit_nest_short_drama_agent", body["agent_name"])
		}
		assetIDs, ok := body["asset_ids"].([]any)
		if !ok || len(assetIDs) != 2 || assetIDs[0] != "asset_1" || assetIDs[1] != "asset_2" {
			t.Fatalf("asset_ids = %#v, want two asset ids", body["asset_ids"])
		}
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"run":{"thread_id":"thread_123","run_id":"run_456"},"web_thread_link":"https://xyq.example/thread_123"}}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"short-drama", "+submit-run",
		"--message", "write a cyberpunk opening",
		"--thread-id", "thread_123",
		"--agent-name", " pippit_nest_short_drama_agent ",
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

func TestShortDramaSubmitRunRequiresMessage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+submit-run"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--message is required") {
		t.Fatalf("error = %q, want message validation", err)
	}
}

func TestShortDramaSubmitRunRequiresAgentName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+submit-run", "--message", "write a cyberpunk opening"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--agent-name is required") {
		t.Fatalf("error = %q, want agent-name validation", err)
	}
}

func TestShortDramaSubmitRunRequiresAccessKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive request without access key")
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommandWithAccessKey(t, &stdout, &stderr, server.URL, "")
	root.SetArgs([]string{
		"short-drama", "+submit-run",
		"--message", "write a cyberpunk opening",
		"--agent-name", "pippit_nest_short_drama_agent",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want access key error")
	}
	if !strings.Contains(err.Error(), "XYQ_ACCESS_KEY is required") {
		t.Fatalf("error = %q, want access key guidance", err)
	}
}

func TestShortDramaUploadFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+upload-file", "--path", "/tmp/story.md"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["scene"] != "short-drama" {
		t.Fatalf("scene = %v, want short-drama", got["scene"])
	}
	if got["status"] != "uploaded" {
		t.Fatalf("status = %v, want uploaded", got["status"])
	}
	if !strings.HasPrefix(got["file_id"].(string), "file_mock_") {
		t.Fatalf("file_id = %v, want mock file id", got["file_id"])
	}
}

func TestShortDramaUploadFileRequiresPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+upload-file"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--path is required") {
		t.Fatalf("error = %q, want path validation", err)
	}
}

func TestShortDramaDownloadResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Pippit-CLI/1.0" {
			t.Fatalf("User-Agent = %q, want Pippit-CLI/1.0", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/image":
			_, _ = w.Write([]byte("image-data"))
		case "/clip.mp4":
			_, _ = w.Write([]byte("video-data"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	outputDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+download-results",
		"--output-dir", outputDir,
		"--workers", "2",
		"--urls", server.URL + "/image?filename=cover.jpeg",
		server.URL + "/clip.mp4",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["output_dir"] != outputDir {
		t.Fatalf("output_dir = %v, want %s", got["output_dir"], outputDir)
	}
	if got["total"] != float64(2) {
		t.Fatalf("total = %v, want 2", got["total"])
	}
	downloaded, ok := got["downloaded"].([]any)
	if !ok || len(downloaded) != 2 {
		t.Fatalf("downloaded = %#v, want two files", got["downloaded"])
	}
	wantFiles := []string{
		filepath.Join(outputDir, "01.jpeg"),
		filepath.Join(outputDir, "02.mp4"),
	}
	for i, want := range wantFiles {
		if downloaded[i] != want {
			t.Fatalf("downloaded[%d] = %v, want %s", i, downloaded[i], want)
		}
	}
	assertFileContent(t, wantFiles[0], "image-data")
	assertFileContent(t, wantFiles[1], "video-data")
}

func TestShortDramaDownloadResultsRequiresURLs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+download-results"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--urls is required") {
		t.Fatalf("error = %q, want urls validation", err)
	}
}

func TestShortDramaDownloadResultsRejectsInvalidScheme(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+download-results", "--urls", "file:///etc/passwd"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want scheme validation error")
	}
	if !strings.Contains(err.Error(), "only http and https are allowed") {
		t.Fatalf("error = %q, want scheme validation", err)
	}
}

func TestShortDramaDownloadResultsAllFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	outputDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+download-results",
		"--output-dir", outputDir,
		"--urls", server.URL + "/notfound",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want all-failed error")
	}
	if !strings.Contains(err.Error(), "all 1 download(s) failed") {
		t.Fatalf("error = %q, want all-failed message", err)
	}
}

func TestShortDramaDownloadResultsDefaultOutputDir(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image-data"))
	}))
	defer server.Close()

	cwd := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd(): %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("Chdir(%s): %v", cwd, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	})

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+download-results",
		"--urls", server.URL + "/image.png",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["output_dir"] != "./xyq_short_drama_output" {
		t.Fatalf("output_dir = %v, want ./xyq_short_drama_output", got["output_dir"])
	}
	assertFileContent(t, filepath.Join(cwd, "xyq_short_drama_output", "01.png"), "image-data")
}

func TestShortDramaGetThread(t *testing.T) {
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

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"short-drama", "+get-thread",
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

func TestShortDramaGetThreadRequiresThreadID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+get-thread"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--thread-id is required") {
		t.Fatalf("error = %q, want thread-id validation", err)
	}
}

func TestShortDramaGetThreadRequiresAccessKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive request without access key")
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommandWithAccessKey(t, &stdout, &stderr, server.URL, "")
	root.SetArgs([]string{"short-drama", "+get-thread", "--thread-id", "thread_123"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want access key error")
	}
	if !strings.Contains(err.Error(), "XYQ_ACCESS_KEY is required") {
		t.Fatalf("error = %q, want access key guidance", err)
	}
}

func newTestRootCommand(t *testing.T, stdout, stderr io.Writer, baseURL string) *cobra.Command {
	t.Helper()
	return newTestRootCommandWithAccessKey(t, stdout, stderr, baseURL, "test-token")
}

func newTestRootCommandWithAccessKey(t *testing.T, stdout, stderr io.Writer, baseURL string, accessKey string) *cobra.Command {
	t.Helper()
	cfg := config.Load()
	cfg.BaseURL = baseURL
	cfg.AccessKey = accessKey
	authAuthorizer := auth.NewManager(cfg)
	client := common.NewHTTPClient(cfg.BaseURL, cfg.HTTPTimeout, common.NewAccessKeyAuthorizer(cfg.AccessKey))
	runner := common.NewRunner(cfg, client, authAuthorizer)
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

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("ReadFile(%s) = %q, want %q", path, string(data), want)
	}
}
