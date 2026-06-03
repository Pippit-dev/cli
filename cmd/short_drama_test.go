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
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/version"
	"github.com/bytedance/sonic"
	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "pippit-cli-test-home")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)

	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

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
		if body["agent_name"] != "pippit_nest_novel_agent" {
			t.Fatalf("agent_name = %v, want pippit_nest_novel_agent", body["agent_name"])
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

func TestRootIncludesUpdateCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	cmd, _, err := root.Find([]string{"update"})
	if err != nil {
		t.Fatalf("Find(update) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "update" {
		t.Fatalf("Find(update) = %#v, want update command", cmd)
	}
}

func TestRootDoesNotIncludeVersionCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	cmd, _, err := root.Find([]string{"version"})
	if err == nil || cmd != root {
		t.Fatalf("Find(version) = (%#v, %v), want unknown command", cmd, err)
	}
}

func TestRootHelpListsSupportedCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{
		"Pippit CLI submits short-drama workflows",
		"short-drama",
		"update",
		"--version",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("help output = %q, want %q", got, want)
		}
	}
	for _, unwanted := range []string{"completion", "version     "} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("help output = %q, should not contain %q", got, unwanted)
		}
	}
}

func TestVersionFlagPrintsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"--version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != version.Current() {
		t.Fatalf("version flag output = %q, want %s", got, version.Current())
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/biz/v1/skill/upload_file" {
			t.Fatalf("path = %s, want upload_file path", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want test bearer token", r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm(): %v", err)
		}
		if got := r.FormValue("accessKey"); got != "" {
			t.Fatalf("accessKey = %q, want empty (auth via header only)", got)
		}
		files := r.MultipartForm.File["file"]
		if len(files) != 1 {
			t.Fatalf("file parts = %d, want 1", len(files))
		}
		if files[0].Filename != "story.docx" {
			t.Fatalf("filename = %q, want story.docx", files[0].Filename)
		}
		if got := files[0].Header.Get("Content-Type"); got != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
			t.Fatalf("Content-Type = %q, want docx content type", got)
		}
		file, err := files[0].Open()
		if err != nil {
			t.Fatalf("Open multipart file: %v", err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("ReadAll multipart file: %v", err)
		}
		if string(data) != "docx-data" {
			t.Fatalf("file content = %q, want docx-data", string(data))
		}
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"pippit_asset_id":"asset_123"}}`))
	}))
	defer server.Close()

	cwd := chdirTemp(t)
	path := filepath.Join(cwd, "story.docx")
	if err := os.WriteFile(path, []byte("docx-data"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{"short-drama", "+upload-file", "--path", path})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["asset_id"] != "asset_123" {
		t.Fatalf("asset_id = %v, want asset_123", got["asset_id"])
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

func TestShortDramaUploadFileRequiresAccessKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive request without access key")
	}))
	defer server.Close()

	cwd := chdirTemp(t)
	path := filepath.Join(cwd, "story.txt")
	if err := os.WriteFile(path, []byte("txt-data"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	var stdout, stderr bytes.Buffer
	root := newTestRootCommandWithAccessKey(t, &stdout, &stderr, server.URL, "")
	root.SetArgs([]string{"short-drama", "+upload-file", "--path", path})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want access key error")
	}
	if !strings.Contains(err.Error(), "XYQ_ACCESS_KEY is required") {
		t.Fatalf("error = %q, want access key guidance", err)
	}
}

func TestShortDramaUploadFileRejectsUnsupportedFileType(t *testing.T) {
	cwd := chdirTemp(t)
	path := filepath.Join(cwd, "story.png")
	if err := os.WriteFile(path, []byte("png-data"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+upload-file", "--path", path})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want file type validation error")
	}
	if !strings.Contains(err.Error(), "only .doc, .docx, and .txt uploads are supported") {
		t.Fatalf("error = %q, want unsupported type validation", err)
	}
}

func TestShortDramaDownloadResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Pippit-CLI/1.0" {
			t.Fatalf("User-Agent = %q, want Pippit-CLI/1.0", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/image":
			_, _ = w.Write([]byte("image-data"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	chdirTemp(t)
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	outputPath := filepath.Join("results", "cover.jpeg")
	root.SetArgs([]string{
		"short-drama", "+download-result",
		"--output-path", outputPath,
		"--workers", "2",
		"--url", server.URL + "/image?filename=ignored.jpeg",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["output_path"] != outputPath {
		t.Fatalf("output_path = %v, want %s", got["output_path"], outputPath)
	}
	if _, ok := got["total"]; ok {
		t.Fatalf("total should not be returned: %#v", got)
	}
	downloaded, ok := got["downloaded"].([]any)
	if !ok || len(downloaded) != 1 {
		t.Fatalf("downloaded = %#v, want one file", got["downloaded"])
	}
	wantFiles := []string{
		outputPath,
	}
	for i, want := range wantFiles {
		if downloaded[i] != want {
			t.Fatalf("downloaded[%d] = %v, want %s", i, downloaded[i], want)
		}
	}
	assertFileContent(t, wantFiles[0], "image-data")
}

func TestShortDramaDownloadResultSkipsExistingFile(t *testing.T) {
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		t.Fatal("server should not receive request when target file already exists")
	}))
	defer server.Close()

	chdirTemp(t)
	outputPath := filepath.Join("results", "cover.jpeg")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("existing-data"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+download-result",
		"--output-path", outputPath,
		"--url", server.URL + "/image",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if serverCalled {
		t.Fatal("server was called, want existing file to skip download")
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["output_path"] != outputPath {
		t.Fatalf("output_path = %v, want %s", got["output_path"], outputPath)
	}
	if _, ok := got["total"]; ok {
		t.Fatalf("total should not be returned: %#v", got)
	}
	alreadyExist, ok := got["already_exist"].([]any)
	if !ok || len(alreadyExist) != 1 || alreadyExist[0] != outputPath {
		t.Fatalf("already_exist = %#v, want existing output path", got["already_exist"])
	}
	assertFileContent(t, outputPath, "existing-data")
}

func TestShortDramaDownloadResultSkipsExistingFileWhenLocalIsFresh(t *testing.T) {
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		t.Fatal("server should not receive request when target file is fresh")
	}))
	defer server.Close()

	chdirTemp(t)
	outputPath := filepath.Join("results", "cover.jpeg")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("existing-data"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	remoteUpdatedAt := int64(1779716734)
	localUpdatedAt := time.Unix(remoteUpdatedAt+10, 0)
	if err := os.Chtimes(outputPath, localUpdatedAt, localUpdatedAt); err != nil {
		t.Fatalf("Chtimes(): %v", err)
	}

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+download-result",
		"--output-path", outputPath,
		"--updated-at", "1779716734",
		"--url", server.URL + "/image",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if serverCalled {
		t.Fatal("server was called, want fresh existing file to skip download")
	}

	got := decodeJSON(t, stdout.Bytes())
	alreadyExist, ok := got["already_exist"].([]any)
	if !ok || len(alreadyExist) != 1 || alreadyExist[0] != outputPath {
		t.Fatalf("already_exist = %#v, want existing output path", got["already_exist"])
	}
	assertFileContent(t, outputPath, "existing-data")
}

func TestShortDramaDownloadResultOverwritesStaleExistingFile(t *testing.T) {
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		if r.URL.Path != "/image" {
			t.Fatalf("path = %s, want /image", r.URL.Path)
		}
		_, _ = w.Write([]byte("new-data"))
	}))
	defer server.Close()

	chdirTemp(t)
	outputPath := filepath.Join("results", "cover.jpeg")
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("existing-data"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	remoteUpdatedAt := int64(1779716734)
	staleUpdatedAt := time.Unix(remoteUpdatedAt-10, 0)
	if err := os.Chtimes(outputPath, staleUpdatedAt, staleUpdatedAt); err != nil {
		t.Fatalf("Chtimes(): %v", err)
	}

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+download-result",
		"--output-path", outputPath,
		"--updated-at", "1779716734",
		"--url", server.URL + "/image",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if !serverCalled {
		t.Fatal("server was not called, want stale existing file to be downloaded")
	}

	got := decodeJSON(t, stdout.Bytes())
	downloaded, ok := got["downloaded"].([]any)
	if !ok || len(downloaded) != 1 || downloaded[0] != outputPath {
		t.Fatalf("downloaded = %#v, want output path", got["downloaded"])
	}
	if _, ok := got["overwritten"]; ok {
		t.Fatalf("overwritten should not be returned: %#v", got)
	}
	assertFileContent(t, outputPath, "new-data")
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("Stat(%s): %v", outputPath, err)
	}
	if info.ModTime().Unix() != remoteUpdatedAt {
		t.Fatalf("mtime = %d, want %d", info.ModTime().Unix(), remoteUpdatedAt)
	}
}

func TestShortDramaDownloadResultDownloadsMetaJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/meta.json" {
			t.Fatalf("path = %s, want meta.json", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	chdirTemp(t)
	outputPath := filepath.Join("results", "meta.json")

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+download-result",
		"--output-path", outputPath,
		"--url", server.URL + "/meta.json",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["output_path"] != outputPath {
		t.Fatalf("output_path = %v, want %s", got["output_path"], outputPath)
	}
	if _, ok := got["total"]; ok {
		t.Fatalf("total should not be returned: %#v", got)
	}
	downloaded, ok := got["downloaded"].([]any)
	if !ok || len(downloaded) != 1 || downloaded[0] != outputPath {
		t.Fatalf("downloaded = %#v, want meta.json output path", got["downloaded"])
	}
	assertFileContent(t, outputPath, `{"ok":true}`)
}

func TestShortDramaDownloadResultRequiresOutputPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+download-result", "--url", "https://example.com/image.png"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--output-path is required") {
		t.Fatalf("error = %q, want output-path validation", err)
	}
}

func TestShortDramaDownloadResultRejectsOutputDirFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+download-result",
		"--output-dir", filepath.Join("results", "image.png"),
		"--url", "https://example.com/image.png",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want unknown flag error")
	}
	if !strings.Contains(err.Error(), "unknown flag: --output-dir") {
		t.Fatalf("error = %q, want output-dir rejection", err)
	}
}

func TestShortDramaDownloadResultRequiresURL(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+download-result", "--output-path", filepath.Join("results", "image.png")})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--url is required") {
		t.Fatalf("error = %q, want url validation", err)
	}
}

func TestShortDramaDownloadResultRejectsInvalidScheme(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+download-result", "--output-path", filepath.Join("results", "image.png"), "--url", "file:///etc/passwd"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want scheme validation error")
	}
	if !strings.Contains(err.Error(), "only http and https are allowed") {
		t.Fatalf("error = %q, want scheme validation", err)
	}
}

func TestShortDramaDownloadResultAllFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	chdirTemp(t)
	clearDailyErrorLog(t)
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+download-result",
		"--output-path", filepath.Join("results", "missing.png"),
		"--url", server.URL + "/notfound",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want all-failed error")
	}
	if !strings.Contains(err.Error(), "all 1 download(s) failed") {
		t.Fatalf("error = %q, want all-failed message", err)
	}
	logs := readDailyErrorLog(t)
	if len(logs) != 1 {
		t.Fatalf("logs = %#v, want one error log", logs)
	}
	fields, ok := logs[0]["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields = %#v, want object", logs[0]["fields"])
	}
	if _, ok := fields["updated_at"]; ok {
		t.Fatalf("updated_at should be omitted when --updated-at is not provided: %#v", fields)
	}
}

func TestShortDramaDownloadResultOutputPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image-data"))
	}))
	defer server.Close()

	chdirTemp(t)

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	outputPath := filepath.Join("custom", "nested", "cover.png")
	root.SetArgs([]string{
		"short-drama", "+download-result",
		"--output-path", outputPath,
		"--url", server.URL + "/image.png",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["output_path"] != outputPath {
		t.Fatalf("output_path = %v, want %s", got["output_path"], outputPath)
	}
	assertFileContent(t, outputPath, "image-data")
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
		if _, ok := body["after_seq"]; ok {
			t.Fatalf("after_seq = %v, want omitted", body["after_seq"])
		}
		if body["version"] != "v2" {
			t.Fatalf("version = %v, want v2", body["version"])
		}
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"readable_text":"Thread: thread_123\n       [assistant] hello"}}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"short-drama", "+get-thread",
		"--thread-id", "thread_123",
		"--run-id", "run_456",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	if got := stdout.String(); got != "Thread: thread_123\n       [assistant] hello\n" {
		t.Fatalf("stdout = %#v, want API readable text", got)
	}
}

func TestShortDramaGetThreadRequiresThreadID(t *testing.T) {
	clearDailyErrorLog(t)

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

	entries := readDailyErrorLog(t)
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1: %#v", len(entries), entries)
	}
	if entries[0]["command"] != "short-drama +get-thread" {
		t.Fatalf("command = %v, want get-thread", entries[0]["command"])
	}
	if entries[0]["error"] != "--thread-id is required" {
		t.Fatalf("error = %v, want thread-id validation", entries[0]["error"])
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

func TestShortDramaListThreadFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/biz/v1/skill/list_thread_file" {
			t.Fatalf("path = %s, want list_thread_file path", r.URL.Path)
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
		if body["page_num"] != float64(2) {
			t.Fatalf("page_num = %v, want 2", body["page_num"])
		}
		if body["page_size"] != float64(10) {
			t.Fatalf("page_size = %v, want 10", body["page_size"])
		}
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"total":1,"files":[{"file_name":"ignored.png","file_path":"results/images/cover.png","download_url":"https://example.com/cover.png","updated_at":1779716734}]}}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"short-drama", "+list-thread-file",
		"--thread-id", "thread_123",
		"--page-num", "2",
		"--page-size", "10",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	if got["total"] != float64(1) {
		t.Fatalf("total = %v, want 1", got["total"])
	}
	wantMessage := "<system-remind>\n- total is below 200; continue querying with current --page-num 2\n</system-remind>"
	if got["message"] != wantMessage {
		t.Fatalf("message = %v, want current page hint", got["message"])
	}
	files, ok := got["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("files = %#v, want one file", got["files"])
	}
	file, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("file = %#v, want object", files[0])
	}
	if _, ok := file["file_name"]; ok {
		t.Fatalf("file_name should not be returned: %#v", file)
	}
	wantPath := "." + string(os.PathSeparator) + filepath.Join("thread_123", "results/images/cover.png")
	if file["file_path"] != wantPath {
		t.Fatalf("file_path = %v, want %s", file["file_path"], wantPath)
	}
	if file["download_url"] != "https://example.com/cover.png" {
		t.Fatalf("download_url = %v, want returned url", file["download_url"])
	}
	if file["updated_at"] != float64(1779716734) {
		t.Fatalf("updated_at = %v, want 1779716734", file["updated_at"])
	}
}

func TestShortDramaListThreadFileMessageWhenPageFull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "list_thread_file") {
			t.Fatalf("path = %s, want list_thread_file path", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ret":"0","errmsg":"","data":{"total":200,"files":[]}}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{
		"short-drama", "+list-thread-file",
		"--thread-id", "thread_123",
		"--page-num", "2",
		"--page-size", "200",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}

	got := decodeJSON(t, stdout.Bytes())
	wantMessage := "<system-remind>\n- total reached 200; query the next page with --page-num 3\n</system-remind>"
	if got["message"] != wantMessage {
		t.Fatalf("message = %v, want next page hint", got["message"])
	}
}

func TestShortDramaListThreadFileRequiresThreadID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"short-drama", "+list-thread-file"})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--thread-id is required") {
		t.Fatalf("error = %q, want thread-id validation", err)
	}
}

func TestShortDramaListThreadFileRejectsPageSizeAboveMax(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{
		"short-drama", "+list-thread-file",
		"--thread-id", "thread_123",
		"--page-size", "201",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "--page-size must be between 1 and 200") {
		t.Fatalf("error = %q, want page-size validation", err)
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
	client := common.NewHTTPClient(cfg.BaseURL, cfg.HTTPTimeout, common.NewAccessKeyAuthorizer(cfg.AccessKey))
	runner := common.NewRunner(cfg, client)
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

func clearDailyErrorLog(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(testHomeDir(t), ".pippit_tool_cli", "logs")); err != nil {
		t.Fatalf("RemoveAll logs: %v", err)
	}
}

func readDailyErrorLog(t *testing.T) []map[string]any {
	t.Helper()
	path := filepath.Join(testHomeDir(t), ".pippit_tool_cli", "logs", time.Now().Format("2006-01-02")+".log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var entry map[string]any
		if err := sonic.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log line: %v\n%s", err, line)
		}
		entries = append(entries, entry)
	}
	return entries
}

func testHomeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir(): %v", err)
	}
	return home
}

func chdirTemp(t *testing.T) string {
	t.Helper()
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
	return cwd
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
