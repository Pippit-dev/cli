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
			if stdout.String() != "{\"asset_id\":\"asset_original\"}\n" || calls != 1 {
				t.Fatalf("stdout = %q, requests = %d", stdout.String(), calls)
			}
		})
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
	if err := root.Execute(); err != nil || !strings.Contains(stdout.String(), "--path") {
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
	for _, response := range []string{`{"ret":"1","errmsg":"rejected"}`, `{"ret":"0","data":{"asset_id":"wrong_field"}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(response))
		}))
		var stdout, stderr bytes.Buffer
		root := newTestRootCommand(t, &stdout, &stderr, server.URL)
		root.SetArgs([]string{"upload-file", "--path", path})
		err := root.Execute()
		server.Close()
		if err == nil || stdout.Len() != 0 {
			t.Errorf("response %s: error = %v, stdout = %s", response, err, stdout.String())
		}
	}
}
