package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGenerateAudioJSONParameters(t *testing.T) {
	for _, tc := range []struct {
		name, source, input, expected, message string
		flags                                  []string
	}{
		{name: "unknown fields and precise values", source: "input", input: `{"model":"  future/model  ","prompt":"  unchanged prompt  ","audio_config_v2":{"sample_rate":24000,"speech_rate":0},"watermark":false,"include":null,"future":{"large":9007199254740993123,"decimal":1.2300,"negative_zero":-0,"list":[false,null,0]}}`, message: "  unchanged prompt  "},
		{name: "file text", source: "file", input: `{"model":"future","text":"read this","references":[{"type":"audio","speaker":"speaker://example/voice"}]}`, message: "read this"},
		{name: "stdin task without prompt", source: "stdin", input: `{"model":"future","task_type":"dubbing","dubbing_config":{"target_language":"en"}}`, message: "音频任务：dubbing"},
		{name: "neutral message preserves null", source: "input", input: `{"model":"future","prompt":null,"text":null,"task_type":null,"audio_config":null,"references":null}`, message: "音频生成任务"},
		{name: "no injected model", source: "input", input: `{"prompt":"hi"}`, message: "hi"},
		{name: "empty object", source: "input", input: `{}`, message: "音频生成任务"},
		{name: "merge disjoint flags", source: "input", input: `{"audio_config":{"future":9007199254740993123}}`, flags: []string{"--model", " Other ", "--prompt", "hello", "--speech-rate", "0", "--enable-timestamp=false"}, expected: `{"model":" Other ","prompt":"hello","audio_config":{"future":9007199254740993123,"speech_rate":0,"enable_timestamp":false}}`, message: "hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/api/biz/v1/skill/submit_run" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				var agent, message string
				_ = json.Unmarshal(body["agent_name"], &agent)
				_ = json.Unmarshal(body["message"], &message)
				if len(body) != 3 || agent != "pippit_audio_part_agent" || message != tc.message {
					t.Errorf("unexpected envelope: %s", body)
				}
				expected := tc.expected
				if expected == "" {
					expected = tc.input
				}
				if !reflect.DeepEqual(audioJSON(t, body["audio_part_tool_param"]), audioJSON(t, []byte(expected))) {
					t.Errorf("parameters=%s, want %s", body["audio_part_tool_param"], expected)
				}
				io.WriteString(w, `{"ret":"0","data":{"run":{"thread_id":"thread","run_id":"run"}}}`)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			args := []string{"generate-audio"}
			switch tc.source {
			case "file":
				path := filepath.Join(t.TempDir(), "params.json")
				if err := os.WriteFile(path, []byte(tc.input), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--file", path)
			case "stdin":
				root.SetIn(strings.NewReader(tc.input))
				args = append(args, "--file", "-")
			default:
				args = append(args, "--input", tc.input)
			}
			root.SetArgs(append(args, tc.flags...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("requests=%d, want one submit and no uploads", requests)
			}
		})
	}
}

func TestGenerateAudioRejectsJSONBeforeUpload(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"both sources", "不能同时", []string{"--input", "{}", "--file", "-"}},
		{"empty sources", "不能同时", []string{"--input=", "--file="}},
		{"empty file", "不能为空", []string{"--file="}},
		{"invalid", "JSON 对象", []string{"--input", "{"}},
		{"array", "JSON 对象", []string{"--input", "[]"}},
		{"null", "JSON 对象", []string{"--input", "null"}},
		{"scalar", "JSON 对象", []string{"--input", "1"}},
		{"trailing", "尾随", []string{"--input", "{} {}"}},
		{"duplicate", "重复字段", []string{"--input", `{"model":"a","model":"b"}`}},
		{"nested duplicate", "重复字段", []string{"--input", `{"future":[{"a":0,"a":1}]}`}},
		{"model conflict", "冲突", []string{"--input", `{"model":null}`, "--model", "future"}},
		{"prompt conflict", "冲突", []string{"--input", `{"prompt":"hello"}`, "--prompt", "hello"}},
		{"config conflict", "冲突", []string{"--input", `{"audio_config":{"enable_timestamp":false}}`, "--enable-timestamp=false"}},
		{"config null", "不是对象", []string{"--input", `{"audio_config":null}`, "--speech-rate", "0"}},
		{"references null", "必须是 JSON 数组", []string{"--input", `{"references":null}`}},
		{"references object", "必须是 JSON 数组", []string{"--input", `{"references":{}}`}},
	} {
		t.Run(tc.name, func(t *testing.T) { rejectAudioBeforeHTTP(t, tc.args, tc.want) })
	}
	for _, key := range []string{"agent_name", "message", "audio_part_tool_param", "video_part_tool_param", "general_agent_settings", "authorization", "AK", "team_id", "TeamID", "headers", "base_url"} {
		t.Run("protected "+key, func(t *testing.T) {
			rejectAudioBeforeHTTP(t, []string{"--input", fmt.Sprintf(`{%q:"value"}`, key)}, "身份字段")
		})
	}
}

func TestGenerateAudioReferenceAppendOrder(t *testing.T) {
	dir := t.TempDir()
	kinds := []string{"video", "audio", "image", "audio"}
	args := []string{"generate-audio", "--input", `{"model":"future","references":[{"type":"audio","speaker":"speaker://example/voice","future":false},{"type":"image","pippit_asset_id":"existing"}]}`}
	for i, kind := range kinds {
		path := filepath.Join(dir, fmt.Sprintf("reference-%d.bin", i))
		if err := os.WriteFile(path, []byte(kind), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "--"+kind, path)
	}
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/biz/v1/skill/upload_file":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			files := r.MultipartForm.File["file"]
			if len(files) != 1 || files[0].Filename != fmt.Sprintf("reference-%d.bin", len(calls)) {
				t.Errorf("upload order: %#v", files)
			}
			calls = append(calls, "upload")
			fmt.Fprintf(w, `{"ret":"0","data":{"pippit_asset_id":"asset_%d"}}`, len(calls))
		case "/api/biz/v1/skill/submit_run":
			calls = append(calls, "submit")
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			params := audioJSON(t, body["audio_part_tool_param"]).(map[string]any)
			refs := params["references"].([]any)
			want := []any{map[string]any{"type": "audio", "speaker": "speaker://example/voice", "future": false}, map[string]any{"type": "image", "pippit_asset_id": "existing"}}
			for i, kind := range kinds {
				want = append(want, map[string]any{"type": kind, "pippit_asset_id": fmt.Sprintf("asset_%d", i+1)})
			}
			if !reflect.DeepEqual(refs, want) {
				t.Errorf("references=%#v, want %#v", refs, want)
			}
			io.WriteString(w, `{"ret":"0","data":{"run":{"thread_id":"thread","run_id":"run"}}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "upload,upload,upload,upload,submit" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestGenerateAudioPreflightsAllLocalFiles(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid.wav")
			if kind == "directory" {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if kind == "unreadable" {
				if err := os.WriteFile(path, []byte("data"), 0o000); err != nil {
					t.Fatal(err)
				}
				if file, err := os.Open(path); err == nil {
					file.Close()
					t.Skip("current user can read mode 000 files")
				}
			}
			rejectAudioBeforeHTTP(t, []string{"--prompt", "hello", "--video", path}, "参考")
		})
	}
}

func TestGenerateAudioStopsAfterSecondUploadFailure(t *testing.T) {
	args := []string{"generate-audio", "--prompt", "use these references"}
	for _, name := range []string{"first.wav", "second.wav", "third.wav"} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "--audio", path)
	}
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/biz/v1/skill/upload_file" || r.Method != http.MethodPost {
			calls = append(calls, r.URL.Path)
			t.Errorf("upload failure must prevent later requests: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		files := r.MultipartForm.File["file"]
		if len(files) != 1 {
			t.Errorf("uploaded files=%d, want one", len(files))
			return
		}
		calls = append(calls, files[0].Filename)
		if files[0].Filename == "second.wav" {
			http.Error(w, "upload unavailable", http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"ret":"0","data":{"pippit_asset_id":"first_asset"}}`)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs(args)
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "上传音频生成参考素材失败") || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("error=%v, want second upload HTTP failure", err)
	}
	if got := strings.Join(calls, ","); got != "first.wav,second.wav" {
		t.Fatalf("requests=%s, want only the first and second uploads", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed upload must not print a submitted task: %s", stdout.String())
	}
}

func rejectAudioBeforeHTTP(t *testing.T, args []string, want string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "readable.wav")
	if err := os.WriteFile(path, []byte("valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Errorf("invalid input reached HTTP: %s", r.URL.Path)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs(append([]string{"generate-audio", "--audio", path}, args...))
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error=%v, want %q", err, want)
	}
	if requests != 0 {
		t.Fatalf("requests=%d, want no requests", requests)
	}
}

func audioJSON(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
