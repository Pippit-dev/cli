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

func TestGenerateAudioRequest(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		model      *string
		files      []string
		flags      []string
		config     map[string]any
	}{
		{name: "text only"},
		{name: "arbitrary model", model: audioString("  future/model  ")},
		{name: "explicit empty model", model: audioString("")},
		{name: "explicit whitespace model", model: audioString("  ")},
		{name: "four audio references", kind: "audio", files: []string{"a.wav", "b.wav", "c.wav", "d.wav"}},
		{name: "two images", kind: "image", files: []string{"a.png", "b.png"}},
		{name: "video", kind: "video", files: []string{"clip.mp4"}},
		{name: "unlisted extension", kind: "audio", files: []string{"audio.custom"}},
		{name: "format and zero rate", flags: []string{"--format", " NewFormat ", "--sample-rate", "0"}, config: map[string]any{"format": " NewFormat ", "sample_rate": float64(0)}},
		{name: "three audio references", kind: "audio", files: []string{"one.wav", "two.mp3", "three.wav"},
			flags:  []string{"--format", "WAV", "--sample-rate", "24000", "--speech-rate", "1.2", "--loudness-rate", "0", "--pitch-rate", "-0.5", "--enable-timestamp=false"},
			config: map[string]any{"format": "WAV", "sample_rate": float64(24000), "speech_rate": 1.2, "loudness_rate": float64(0), "pitch_rate": -0.5, "enable_timestamp": false}},
		{name: "one image reference", kind: "image", files: []string{"scene.png"}, flags: []string{"--format", "ogg_opus"}, config: map[string]any{"format": "ogg_opus"}},
		{name: "pcm", flags: []string{"--format", "pcm"}, config: map[string]any{"format": "pcm"}},
		{name: "mp3 timestamp", flags: []string{"--format", "mp3", "--enable-timestamp"}, config: map[string]any{"format": "mp3", "enable_timestamp": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var uploads int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" || r.Method != http.MethodPost {
					t.Errorf("unexpected request auth/method: %s %s", r.Method, r.Header.Get("Authorization"))
				}
				switch r.URL.Path {
				case "/api/biz/v1/skill/upload_file":
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Errorf("parse multipart: %v", err)
						return
					}
					defer r.MultipartForm.RemoveAll()
					files := r.MultipartForm.File["file"]
					if len(files) != 1 || uploads >= len(tc.files) || files[0].Filename != tc.files[uploads] {
						t.Errorf("unexpected uploaded files: %#v", files)
						return
					}
					file, err := files[0].Open()
					if err != nil {
						t.Errorf("open upload: %v", err)
						return
					}
					data, err := io.ReadAll(file)
					file.Close()
					if err != nil || string(data) != "reference data" {
						t.Errorf("upload contents = %q, err = %v", data, err)
					}
					uploads++
					fmt.Fprintf(w, `{"ret":"0","data":{"pippit_asset_id":"asset_%d"}}`, uploads)
				case "/api/biz/v1/skill/submit_run":
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode submit: %v", err)
						return
					}
					if len(body) != 3 || body["agent_name"] != "pippit_audio_part_agent" || body["message"] != "  温暖的旁白  " {
						t.Errorf("unexpected submit body: %#v", body)
					}
					param, ok := body["audio_part_tool_param"].(map[string]any)
					wantModel := "seedaudio_1.0"
					if tc.model != nil {
						wantModel = *tc.model
					}
					if !ok || param["model"] != wantModel || param["prompt"] != body["message"] {
						t.Errorf("unexpected audio params: %#v", param)
					}
					if tc.config == nil {
						if _, ok := param["audio_config"]; ok {
							t.Errorf("unset audio_config must be omitted: %#v", param)
						}
					} else if !reflect.DeepEqual(param["audio_config"], tc.config) {
						t.Errorf("config = %#v, want %#v", param["audio_config"], tc.config)
					}
					if _, ok := param["text"]; ok {
						t.Errorf("unsupported text must be omitted: %#v", param)
					}
					refs, _ := param["references"].([]any)
					if len(refs) != len(tc.files) || uploads != len(tc.files) {
						t.Errorf("references=%#v, uploads=%d", refs, uploads)
					}
					for i, ref := range refs {
						want := map[string]any{"type": tc.kind, "pippit_asset_id": fmt.Sprintf("asset_%d", i+1)}
						if !reflect.DeepEqual(ref, want) {
							t.Errorf("reference = %#v, want %#v", ref, want)
						}
					}
					io.WriteString(w, `{"ret":"0","data":{"run":{"thread_id":"thread_123","run_id":"run_456"},"web_thread_link":"https://example.com/thread_123"}}`)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			args := []string{"generate-audio", "--prompt", "  温暖的旁白  "}
			if tc.model != nil {
				args = append(args, "--model", *tc.model)
			}
			for _, name := range tc.files {
				path := filepath.Join(t.TempDir(), name)
				if err := os.WriteFile(path, []byte("reference data"), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--"+tc.kind, path)
			}
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			root.SetArgs(append(args, tc.flags...))
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			got := decodeJSON(t, stdout.Bytes())
			if got["thread_id"] != "thread_123" || got["run_id"] != "run_456" || got["web_thread_link"] != "https://example.com/thread_123" {
				t.Fatalf("unexpected result: %#v", got)
			}
		})
	}
}

func TestGenerateAudioRejectsInvalidInputsBeforeHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		args       []string
	}{
		{"prompt", "--prompt", []string{}},
		{"missing file", "读取参考文件失败", []string{"--prompt", "x", "--audio", filepath.Join(t.TempDir(), "missing.wav")}},
		{"nan", "有限数值", []string{"--prompt", "x", "--speech-rate", "NaN"}},
		{"infinity", "有限数值", []string{"--prompt", "x", "--pitch-rate", "+Inf"}},
		{"duration", "未知参数", []string{"--prompt", "x", "--duration", "5"}},
		{"text", "未知参数", []string{"--prompt", "x", "--text", "hello"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("invalid request reached HTTP: %s", r.URL.Path)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			root.SetArgs(append([]string{"generate-audio"}, tc.args...))
			if err := root.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Execute error=%v, want %s", err, tc.want)
			}
		})
	}
}

func TestGenerateAudioServerFailurePreservesLogID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ret":"5","errmsg":"音频生成不可用","log_id":"audio_log_123"}`)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{"generate-audio", "--prompt", "x"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "音频生成不可用") || !strings.Contains(err.Error(), "audio_log_123") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func audioString(value string) *string { return &value }
