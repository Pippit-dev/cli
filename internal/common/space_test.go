package common

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/space"
)

type spaceRewriteAdapter struct{ adapter.Default }

func (spaceRewriteAdapter) PrepareAPIRequest(_ context.Context, r *http.Request) (adapter.AuthDecision, error) {
	r.URL.Path = "/rewritten" + r.URL.Path
	return adapter.UseDefaultAuth, nil
}

func TestSpaceHeadersAndTaskRecoveryThroughHTTP(t *testing.T) {
	var headers []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get("X-Team-Id"))
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing authorization")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		switch r.URL.Path {
		case config.UploadFilePath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"pippit_asset_id":"asset"}}`))
		case config.SubmitRunPath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"run":{"thread_id":"thread","run_id":"run"}}}`))
		default:
			_, _ = w.Write([]byte(`{"ret":"0","data":{}}`))
		}
	}))
	defer api.Close()
	store := &space.Store{Dir: t.TempDir(), Account: "account", Origin: api.URL}
	if err := store.SaveDefault(space.Selection{TeamID: "12", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	account, identityChecks := store.Account, 0
	ctx := WithSpace(context.Background(), &space.Session{Store: store, Identity: func(context.Context) (string, error) {
		identityChecks++
		return account, nil
	}})
	client := NewHTTPClient(api.URL, time.Second, NewAccessKeyAuthorizer("test-key"))
	file := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(file, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := client.SendMultipartRequest(ctx, config.UploadFilePath, nil, MultipartFile{FieldName: "file", Path: file}, &result); err != nil {
		t.Fatal(err)
	}
	if identityChecks != 1 {
		t.Fatalf("identity checks per request = %d, want 1", identityChecks)
	}
	if err := store.SaveDefault(space.Selection{TeamID: "34", Name: "B"}); err != nil {
		t.Fatal(err)
	}
	if err := client.SendRequest(ctx, config.SubmitRunPath, map[string]any{"asset_ids": []string{"asset"}}, &result); err != nil {
		t.Fatal(err)
	}
	// A new command recovers the original task even after a saved-space switch.
	queryCtx := WithSpace(context.Background(), &space.Session{Store: store})
	if err := client.SendRequest(queryCtx, api.URL+config.GetThreadPath, space.References{ThreadID: "thread", RunID: "run"}, &result); err != nil {
		t.Fatal(err)
	}
	if err := client.SendRequestWithHeaders(ctx, config.GetAvailableModelListPath, nil, map[string]string{"X-Team-Id": "999"}, &result); err != nil {
		t.Fatal(err)
	}
	zero := "0"
	personal := WithSpace(context.Background(), &space.Session{Store: store, Explicit: &zero})
	if err := client.SendRequest(personal, config.GetCreditBalancePath, struct{}{}, &result); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"12", "12", "12", "12", ""} {
		if headers[i] != want {
			t.Errorf("request %d team = %q, want %q", i, headers[i], want)
		}
	}
	other := "34"
	conflict := WithSpace(context.Background(), &space.Session{Store: store, Explicit: &other})
	if err := client.SendRequest(conflict, config.GetThreadPath, space.References{ThreadID: "thread", RunID: "run"}, &result); err == nil {
		t.Fatal("conflict sent")
	}
	if len(headers) != 5 {
		t.Fatal("conflict reached API")
	}
	// A new API path still receives the selected space without a client allowlist.
	unlisted := WithSpace(context.Background(), &space.Session{Store: store})
	if err := client.SendRequest(unlisted, "/api/unlisted", struct{}{}, &result); err != nil {
		t.Fatal(err)
	}
	if headers[len(headers)-1] != "34" {
		t.Fatal("unlisted API did not receive the saved team")
	}
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Team-Id") != "" || r.Header.Get("Authorization") != "" {
			t.Error("foreign origin received sensitive headers")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer foreign.Close()
	// A matching path on another origin must bypass both scope lookup and headers.
	if err := client.SendRequestWithHeaders(conflict, foreign.URL+config.GetThreadPath, space.References{ThreadID: "thread", RunID: "run"}, map[string]string{"X-Team-Id": "999"}, &result); err != nil {
		t.Fatal(err)
	}
	// Path rewriting by an adapter must preserve the original operation's scope.
	rewritten := NewHTTPClientWithAdapter(api.URL, time.Second, NewAccessKeyAuthorizer("test-key"), spaceRewriteAdapter{})
	if err := rewritten.SendRequest(ctx, config.GetCreditBalancePath, struct{}{}, &result); err != nil {
		t.Fatal(err)
	}
	if headers[len(headers)-1] != "12" {
		t.Fatal("adapter dropped space")
	}
	// Even with a frozen space, a changed account must stop before sending.
	account = "other-account"
	before := len(headers)
	if err := client.SendRequest(ctx, config.GetCreditBalancePath, struct{}{}, &result); err == nil {
		t.Fatal("account switch accepted")
	}
	if len(headers) != before {
		t.Fatal("account switch reached API")
	}
}
