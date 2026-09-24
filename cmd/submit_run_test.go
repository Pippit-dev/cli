package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

func TestSubmitRunCommandPreservesScriptContract(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing_with_assets"}[existing], func(t *testing.T) {
			message := "  根据参考素材生成视频\n保留原文  "
			want := map[string]any{"message": message}
			args := []string{"submit-run", "--message", message, "--source", ""}
			if existing {
				want["thread_id"] = "skill_original_thread"
				want["asset_ids"] = []any{"asset_original_1", "asset_original_2"}
				args = append(args, "--thread-id", "skill_original_thread", "--asset-ids", "asset_original_1", "--asset-ids", "asset_original_2")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != config.SubmitRunPath {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing bearer authorization")
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body = %#v, want %#v", got, want)
				}
				_, _ = w.Write([]byte(`{"ret":"0","data":{"run":{"thread_id":"thread_1","run_id":"run_1"},"web_thread_link":"https://xyq.jianying.com/thread_1"}}`))
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if got := stdout.String(); got != "{\"thread_id\":\"thread_1\",\"run_id\":\"run_1\",\"web_thread_link\":\"https://xyq.jianying.com/thread_1\"}\n" {
				t.Fatalf("unexpected output: %s", got)
			}
		})
	}
}

func TestSubmitRunCommandValidatesBeforeRequest(t *testing.T) {
	for _, args := range [][]string{
		{"submit-run"},
		{"submit-run", "--message", " \n "},
		{"submit-run", "--message", "hello", "unexpected"},
	} {
		var stdout, stderr bytes.Buffer
		root := newRootCommand(&stdout, &stderr, nil)
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Fatalf("Execute(%v) should fail before accessing the runner", args)
		}
	}
}

func TestSubmitRunHelpDoesNotRequireCredentials(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newRootCommand(&stdout, &stderr, nil)
	root.SetArgs([]string{"submit-run", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--message", "--thread-id", "--asset-ids"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Errorf("help missing %s", flag)
		}
	}
}

func TestSubmitRunCommandRejectsInvalidResponse(t *testing.T) {
	for _, response := range []string{
		`{"ret":"1","errmsg":"rejected"}`,
		`{"ret":"0","data":{"run":{"run_id":"run_1"}}}`,
		`{"ret":"0","data":{"run":{"thread_id":"thread_1"}}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(response))
		}))
		var stdout, stderr bytes.Buffer
		root := newTestRootCommand(t, &stdout, &stderr, server.URL)
		root.SetArgs([]string{"submit-run", "--message", "hello"})
		err := root.Execute()
		server.Close()
		if err == nil || stdout.Len() != 0 {
			t.Fatalf("response %s: error = %v, stdout = %s", response, err, stdout.String())
		}
	}
}
