package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

func TestUploadFileMediaContract(t *testing.T) {
	for _, name := range []string{"image.png", "image.WEBP", "video.mp4", "audio.mp3", "audio.WAV"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, []byte("media-content"), 0o600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != config.UploadFilePath {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing bearer authorization")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				if bytes.Contains(body, []byte("test-token")) || bytes.Contains(body, []byte(`name="accessKey"`)) {
					t.Error("credentials must not appear in multipart body")
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					return
				}
				defer r.MultipartForm.RemoveAll()
				files := r.MultipartForm.File["file"]
				if len(files) != 1 || files[0].Filename != name {
					t.Errorf("unexpected file parts: %#v", files)
					return
				}
				contentType := files[0].Header.Get("Content-Type")
				prefix := "audio/"
				if strings.HasPrefix(name, "image") {
					prefix = "image/"
				} else if strings.HasPrefix(name, "video") {
					prefix = "video/"
				}
				if !strings.HasPrefix(contentType, prefix) {
					t.Errorf("content type = %s, want %s", contentType, prefix)
				}
				file, err := files[0].Open()
				if err != nil {
					t.Error(err)
					return
				}
				defer file.Close()
				data, err := io.ReadAll(file)
				if err != nil || string(data) != "media-content" {
					t.Errorf("uploaded content = %q, err = %v", data, err)
				}
				_, _ = w.Write([]byte(`{"ret":"0","data":{"pippit_asset_id":"asset_original"}}`))
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			root.SetArgs([]string{"upload-file", "--path", path})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			var output map[string]string
			if json.Unmarshal(stdout.Bytes(), &output) != nil || output["asset_id"] != "asset_original" || output["team_id"] != "0" || calls != 1 {
				t.Fatalf("stdout = %q, requests = %d", stdout.String(), calls)
			}
		})
	}
}

func TestUploadFileURLContract(t *testing.T) {
	const sourceURL = "https://assets.example.invalid/no-extension?X-Amz-Signature=keep%2Fexact&x=1"
	for _, teamID := range []string{"0", "12", "34"} {
		t.Run(teamID, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != config.UploadFilePath {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				wantTeamHeader := teamID
				if teamID == "0" {
					wantTeamHeader = ""
				}
				if r.Header.Get("X-Team-Id") != wantTeamHeader || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("URL upload lost selected space or authentication")
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected Content-Type: %s", r.Header.Get("Content-Type"))
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body) != 1 || body["url"] != sourceURL {
					t.Errorf("request must contain only the unchanged URL: %#v", body)
				}
				_, _ = w.Write([]byte(`{"ret":"0","data":{"asset_id":"source-id","pippit_asset_id":"asset-url"}}`))
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			// The origin is deliberately unreachable: only the API may fetch it.
			root.SetArgs([]string{"upload-file", "--url", " " + sourceURL + " ", "--team-id", teamID})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			var output map[string]string
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil || len(output) != 2 || output["asset_id"] != "asset-url" || output["team_id"] != teamID || calls != 1 {
				t.Fatalf("stdout = %q, requests = %d, err = %v", stdout.String(), calls, err)
			}
			if strings.Contains(stdout.String()+stderr.String(), "keep%2Fexact") {
				t.Error("command output exposed the source signature")
			}
		})
	}
}

func TestUploadFileRejectsInvalidSourcesBeforeRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "必须且只能提供一个"},
		{[]string{"--url", " "}, "必须且只能提供一个"},
		{[]string{"--path", "missing.png", "--url", "https://example.com/media"}, "必须且只能提供一个"},
		{[]string{"--url", "http://example.com/media"}, "HTTPS"},
		{[]string{"--url", "file:///tmp/media.png"}, "HTTPS"},
		{[]string{"--url", "https://user:secret@example.com/media"}, "HTTPS"},
		{[]string{"--url", "https://example.com/media#secret"}, "HTTPS"},
		{[]string{"--url", "https://example.com/%zz?signature=secret"}, "HTTPS"},
	} {
		var stdout, stderr bytes.Buffer
		root := newTestRootCommand(t, &stdout, &stderr, server.URL)
		root.SetArgs(append([]string{"upload-file"}, tc.args...))
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("args=%v: error=%v, want %s", tc.args, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Error("validation error exposed URL credentials")
		}
		if stdout.Len() != 0 || calls != 0 {
			t.Fatalf("invalid sources produced output or a request: stdout=%s requests=%d", stdout.String(), calls)
		}
	}
}

func TestValidateMediaUpload(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		size int64
		ok   bool
	}{
		{"below-limit.png", maxMediaUploadBytes - 1, true},
		{"at-limit.png", maxMediaUploadBytes, false},
		{"over-limit.png", maxMediaUploadBytes + 1, false},
		{"script.docx", 1, false},
		{"audio.flac", 1, false},
		{"file.unknown", 1, false},
	} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		// Sparse files exercise the exact size boundary without large allocations.
		if err := os.Truncate(path, tc.size); err != nil {
			t.Fatal(err)
		}
		if err := validateMediaUpload(path); (err == nil) != tc.ok {
			t.Errorf("validateMediaUpload(%s) = %v, want accepted=%t", tc.name, err, tc.ok)
		}
	}
	for _, path := range []string{"", dir, filepath.Join(dir, "missing.png")} {
		if err := validateMediaUpload(path); err == nil {
			t.Errorf("validateMediaUpload(%q) should fail", path)
		}
	}
}

func TestUploadFileCommandHelpAndArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newRootCommand(&stdout, &stderr, nil)
	root.SetArgs([]string{"upload-file", "--help"})
	if err := root.Execute(); err != nil || !strings.Contains(stdout.String(), "--path") || !strings.Contains(stdout.String(), "--url") {
		t.Fatalf("help must work without credentials: %v, %s", err, stdout.String())
	}
	for _, args := range [][]string{{"upload-file"}, {"upload-file", "--path", " "}, {"upload-file", "unexpected"}} {
		root := newRootCommand(&stdout, &stderr, nil)
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Errorf("Execute(%v) should fail", args)
		}
	}
}

func TestUploadFileRejectsInvalidResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, response := range []string{`{"ret":"1","errmsg":"rejected","log_id":"upload-log"}`, `{"ret":"0","data":{"asset_id":"wrong_field"},"log_id":"upload-log"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(response))
		}))
		for _, source := range [][]string{{"--path", path}, {"--url", "https://example.com/media"}} {
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			root.SetArgs(append([]string{"upload-file"}, source...))
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "upload-log") || stdout.Len() != 0 {
				t.Errorf("response %s: error = %v, stdout = %s", response, err, stdout.String())
			}
		}
		server.Close()
	}
}
