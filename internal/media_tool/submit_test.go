package media_tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/commandnames"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	gen "github.com/Pippit-dev/pippit-cli/internal/generate_video"
	"github.com/Pippit-dev/pippit-cli/internal/space"
)

func TestRejectBeforeRequest(t *testing.T) {
	for _, opts := range []Options{
		{Tool: "unknown", AssetID: "asset"}, {Tool: commandnames.ImageSuperResolution},
		{Tool: commandnames.ImageSuperResolution, AssetID: "asset", Quality: "auto"},
		{Tool: commandnames.ImageSuperResolution, AssetID: "asset", BackgroundColor: "green"},
		{Tool: commandnames.VideoMatting, AssetID: "asset", BackgroundColor: "transparent"},
		{Tool: commandnames.VideoMatting, AssetID: "asset", Quality: "HQ"},
	} {
		if _, err := BuildRequest(opts); err == nil {
			t.Fatalf("accepted unsupported options: %+v", opts)
		}
	}
}

func TestSubmitQueryDownload(t *testing.T) {
	for _, tool := range []string{commandnames.ImageSuperResolution, commandnames.VideoMatting} {
		for _, team := range []string{"0", "123"} {
			t.Run(tool+team, func(t *testing.T) {
				calls := 0
				published := false
				var threadID, runID string
				media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "" || r.Header.Get("X-Team-Id") != "" {
						t.Error("download leaked identity")
					}
					fmt.Fprint(w, "real fixture bytes")
				}))
				defer media.Close()
				var server *httptest.Server
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/media" {
						if r.Header.Get("Authorization") != "" {
							t.Error("download leaked credential")
						}
						fmt.Fprint(w, "real fixture bytes")
						return
					}
					if r.Header.Get("Authorization") != "Bearer test-key" {
						t.Error("missing auth")
					}
					expectedTeam := team
					if team == "0" {
						expectedTeam = ""
					}
					if r.Header.Get("X-Team-Id") != expectedTeam {
						t.Errorf("team header %q want %s", r.Header.Get("X-Team-Id"), team)
					}
					if strings.HasSuffix(r.URL.Path, "/submit_run") {
						calls++
						if !published {
							t.Error("recovery IDs not published before sending")
						}
						if r.URL.Query().Get("platform") != "codex" {
							t.Error("host attribution missing")
						}
						var req Request
						if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
							t.Error(err)
						}
						threadID, runID = req.Message.ThreadID, req.Message.RunID
						if len(threadID) != 36 || threadID == runID || len(req.Message.Content) != 1 {
							t.Errorf("bad request %+v", req)
						}
						if tool == commandnames.ImageSuperResolution {
							if !strings.Contains(req.Message.Content[0].Data, `"prompt_template":"tool/image_super_resolution"`) {
								t.Error("wrong image template")
							}
						} else if !strings.Contains(req.Message.Content[0].Data, `video_matting`) {
							t.Error("wrong video tool")
						}
						json.NewEncoder(w).Encode(map[string]any{"ret": "0", "data": map[string]any{"run": map[string]string{"thread_id": threadID, "run_id": runID}}})
						return
					}
					subtype := "biz/x_data_image"
					data := map[string]any{"image": map[string]string{"url": media.URL + "/media", "pippit_asset_id": "result-asset"}}
					if tool == commandnames.VideoMatting {
						subtype = "biz/x_data_video"
						data = map[string]any{"video": map[string]string{"download_url": media.URL + "/media", "vid": "video-result"}}
					}
					raw, _ := json.Marshal(data)
					content := map[string]any{"sub_type": subtype, "data": string(raw)}
					entry := map[string]any{"artifact": map[string]any{"content": []any{content}}}
					run := map[string]any{"run_id": runID, "state": 3, "entry_list": []any{entry}}
					thread := map[string]any{"thread_id": threadID, "run_list": []any{run}}
					json.NewEncoder(w).Encode(map[string]any{"ret": "0", "data": map[string]any{"thread": thread}})
				}))
				defer server.Close()
				// Downloads from an untrusted origin use the same client but no credentials.
				client := common.NewHTTPClient(server.URL, time.Second, common.NewAccessKeyAuthorizer("test-key"))
				runner := &common.Runner{Client: client}
				ctx := common.WithSpace(context.Background(), &space.Session{Explicit: &team})
				result, err := Submit(ctx, Options{Tool: tool, AssetID: "source-asset", Source: "codex"}, runner, func(r *Result) error { published = r.ThreadID != "" && r.RunID != ""; return nil })
				if err != nil || result.Status != "submitted" || result.TeamID != team || calls != 1 {
					t.Fatalf("submit result %+v error %v calls %d", result, err, calls)
				}
				queried, err := gen.QueryResult(ctx, &gen.QueryResultOptions{ThreadID: result.ThreadID, RunID: result.RunID, DownloadDir: t.TempDir()}, runner)
				if err != nil {
					t.Fatal(err)
				}
				if queried.Status != "succeeded" || queried.TeamID != team {
					t.Fatalf("bad query %+v", queried)
				}
				var path string
				if tool == commandnames.ImageSuperResolution {
					path = queried.Images[0].OutputPath
					if filepath.Base(path) != "result-asset.png" {
						t.Fatal(path)
					}
				} else {
					path = queried.Videos[0].OutputPath
				}
				bytes, err := os.ReadFile(path)
				if err != nil || string(bytes) != "real fixture bytes" {
					t.Fatalf("download bytes %q error %v", bytes, err)
				}
			})
		}
	}
}

func TestUncertainSubmissionPreservesIDsAndDoesNotRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	runner := &common.Runner{Client: common.NewHTTPClient(server.URL, time.Second, common.NewAccessKeyAuthorizer("test-key"))}
	var before Result
	result, err := Submit(context.Background(), Options{Tool: commandnames.VideoMatting, AssetID: "asset"}, runner, func(r *Result) error { before = *r; return nil })
	if err == nil || result.Status != "submission_unknown" || result.ThreadID != before.ThreadID || result.RunID != before.RunID || calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestSubmissionErrorsKeepDiagnostics(t *testing.T) {
	for _, tc := range []struct{ body, status string }{
		{`{"ret":"403","errmsg":"asset outside space","log_id":"log-denied"}`, "rejected"},
		{`{"ret":"0","log_id":"log-mismatch","data":{"run":{"thread_id":"other-thread","run_id":"other-run"}}}`, "submission_unknown"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			result, err := Submit(context.Background(), Options{Tool: commandnames.ImageSuperResolution, AssetID: "asset"}, &common.Runner{Client: common.NewHTTPClient(server.URL, time.Second, common.NewAccessKeyAuthorizer("key"))}, nil)
			if err == nil || result.Status != tc.status || result.ThreadID == "" || result.RunID == "" || result.LogID == "" {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if tc.status == "submission_unknown" && (result.ResponseThreadID != "other-thread" || result.ResponseRunID != "other-run" || result.ThreadID == result.ResponseThreadID) {
				t.Fatalf("lost known identifiers: %+v", result)
			}
		})
	}
}

func TestKnownCrossSpaceAssetRejectedBeforeSend(t *testing.T) {
	store, err := space.NewStore("https://example.com", "account")
	if err != nil {
		t.Fatal(err)
	}
	store.Dir = t.TempDir()
	if err := store.Remember(space.References{AssetIDs: []string{"asset"}}, space.Selection{TeamID: "42"}); err != nil {
		t.Fatal(err)
	}
	team := "43"
	ctx := common.WithSpace(context.Background(), &space.Session{Explicit: &team, Store: store})
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	result, err := Submit(ctx, Options{Tool: commandnames.VideoMatting, AssetID: "asset"}, &common.Runner{Client: common.NewHTTPClient(server.URL, time.Second, common.NewAccessKeyAuthorizer("key"))}, nil)
	if err == nil || result != nil || calls != 0 || !strings.Contains(err.Error(), "素材属于空间") {
		t.Fatalf("result=%+v error=%v calls=%d", result, err, calls)
	}
}

func TestHTTPAuthRejectionPreservesRecoveryAndLogID(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("X-Tt-Logid", "auth-rejected-log")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"ret":"1015","errmsg":"invalid access key"}`)
			}))
			defer server.Close()
			result, err := Submit(context.Background(), Options{Tool: commandnames.ImageSuperResolution, AssetID: "asset"}, &common.Runner{Client: common.NewHTTPClient(server.URL, time.Second, common.NewAccessKeyAuthorizer("invalid"))}, nil)
			if err == nil || result.Status != "rejected" || result.LogID != "auth-rejected-log" || result.ThreadID == "" || result.RunID == "" || calls != 1 || strings.Contains(result.ErrorMessage, "查询原") {
				t.Fatalf("result=%+v error=%v calls=%d", result, err, calls)
			}
		})
	}
}
