package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	canvascore "github.com/Pippit-dev/pippit-cli/internal/canvas"
)

func TestCanvasUploadURLContract(t *testing.T) {
	const sourceURL = "https://assets.example.invalid/no-extension?signature=keep%2Fexact&x=1"
	for _, teamID := range []string{"0", "12", "34"} {
		t.Run(teamID, func(t *testing.T) {
			uploads, queries := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wantTeamHeader := teamID
				if teamID == "0" {
					wantTeamHeader = ""
				}
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" ||
					r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Team-Id") != wantTeamHeader {
					t.Error("upload and query must retain JSON transport, authentication and the selected space")
				}
				switch r.URL.Path {
				case canvascore.UploadPath:
					uploads++
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body) != 1 || body["url"] != sourceURL {
						t.Errorf("upload body = %#v, want only the unchanged URL", body)
					}
					_, _ = w.Write([]byte(`{"ret":"0","log_id":"upload-log","data":{"asset_id":"source-id","pippit_asset_id":"asset-url"}}`))
				case canvascore.QueryPath:
					queries++
					var body struct {
						IDs []string `json:"pippit_asset_ids"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) != 1 || body.IDs[0] != "asset-url" {
						t.Errorf("query body = %#v, err = %v", body, err)
					}
					_, _ = w.Write([]byte(`{"ret":"0","log_id":"query-log","data":{"Assets":[{"PippitAssetID":"asset-url","SourceID":"source-id","AssetType":1,"Image":{"asset_id":"media-id"}}]}}`))
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.Error(w, "unexpected request", http.StatusBadRequest)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			root := newTestRootCommand(t, &stdout, &stderr, server.URL)
			// An unreachable origin ensures only the API receives the source URL.
			root.SetArgs([]string{"canvas", "upload", "--url", " " + sourceURL + " ", "--team-id", teamID})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			var result canvascore.UploadResult
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if uploads != 1 || queries != 1 || result.State != canvascore.StateReady || result.PollAttempts != 1 ||
				result.AssetID != "source-id" || result.SourceID != "source-id" || result.PippitAssetID != "asset-url" ||
				result.MediaAssetID != "media-id" || result.Locator.PippitAssetID != "asset-url" || result.Locator.MediaAssetID != "media-id" ||
				result.LogID != "upload-log" || result.QueryLogID != "query-log" {
				t.Fatalf("uploads=%d queries=%d result=%+v", uploads, queries, result)
			}
			if strings.Contains(stdout.String()+stderr.String(), "keep%2Fexact") {
				t.Error("output exposed the source signature")
			}
		})
	}
}

func TestCanvasUploadRejectsInvalidSourcesBeforeRequest(t *testing.T) {
	clearDailyErrorLog(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer server.Close()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "必须且只能提供一个"},
		{[]string{"--url", " "}, "必须且只能提供一个"},
		{[]string{"--path", "missing.png", "--url", "https://example.com/media?signature=secret"}, "必须且只能提供一个"},
		{[]string{"--url", "http://example.com/media"}, "HTTPS"},
		{[]string{"--url", "file:///tmp/media.png"}, "HTTPS"},
		{[]string{"--url", "https://user:secret@example.com/media"}, "HTTPS"},
		{[]string{"--url", "https://example.com/media#secret"}, "HTTPS"},
		{[]string{"--url", "https://example.com/%zz?signature=secret"}, "HTTPS"},
	} {
		var stdout, stderr bytes.Buffer
		root := newTestRootCommand(t, &stdout, &stderr, server.URL)
		root.SetArgs(append([]string{"canvas", "upload"}, tc.args...))
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("error=%v, want %s", err, tc.want)
		}
		if strings.Contains(err.Error()+stderr.String(), "secret") || stdout.Len() != 0 || calls != 0 {
			t.Fatal("invalid input exposed the URL or produced output/requests")
		}
	}
	logs, err := json.Marshal(readDailyErrorLog(t))
	if err != nil || bytes.Contains(logs, []byte("secret")) {
		t.Fatalf("validation log exposed the source URL, marshal error=%v", err)
	}
}

func TestCanvasUploadHelpExposesBothSources(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newRootCommand(&stdout, &stderr, nil)
	root.SetArgs([]string{"canvas", "upload", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--path", "--url", "--poll-interval", "--timeout"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Errorf("help missing %s", flag)
		}
	}
}
