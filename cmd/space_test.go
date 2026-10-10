package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/Pippit-dev/pippit-cli/internal/space"
)

func TestSpaceCommandsAndBusinessTaskScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	var requests []string
	var requestRef space.References
	listFailure := false
	listTeamID := ""
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path+":"+r.Header.Get("X-Team-Id"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == config.GetLiteTeamListPath {
			if r.Header.Get("X-Team-Id") != listTeamID {
				t.Errorf("team list header = %q, want %q", r.Header.Get("X-Team-Id"), listTeamID)
			}
			var body struct {
				Cursor string `json:"cursor"`
				Count  int    `json:"count"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Count != 100 {
				t.Error("unexpected page size")
			}
			if listFailure {
				_, _ = w.Write([]byte(`{"ret":"101","errmsg":"denied"}`))
				return
			}
			if body.Cursor == "0" {
				_, _ = w.Write([]byte(`{"ret":"0","data":{"team_info_list":[{"team_id":"12","team_name":"A","current_role":1}],"has_more":true,"next_cursor":"next"}}`))
			} else {
				_, _ = w.Write([]byte(`{"ret":"0","data":{"team_info_list":[{"team_id":"34","team_name":"B"}],"has_more":false}}`))
			}
			return
		}
		if r.URL.Path == config.UploadFilePath {
			_, _ = io.Copy(io.Discard, r.Body)
		} else {
			requestRef = space.References{}
			if err := json.NewDecoder(r.Body).Decode(&requestRef); err != nil {
				t.Error(err)
			}
		}
		switch r.URL.Path {
		case config.SubmitRunPath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"run":{"thread_id":"thread-a","run_id":"run-a"}}}`))
		case config.GetThreadPath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"readable_text":"task result"}}`))
		case config.ListThreadFilePath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"files":[],"total":0}}`))
		case config.UploadFilePath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"pippit_asset_id":"asset-a"}}`))
		case marketingPaths["generate"]:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"run":{"thread_id":"marketing-thread","run_id":"marketing-run"}}}`))
		case marketingPaths["query"]:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"thread_id":"marketing-thread","run_id":"marketing-run","run_state":2}}`))
		case config.GetCreditBalancePath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"total_remain_amount":"100"}}`))
		case config.GetAvailableModelListPath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"scene":"web_turbo_video_generator","config_key":"key","config":{"models":[{"key":"MiniMax-H3","name":"MiniMax","kind":"video","supported_ratio_list":[0,2,3],"default_ratio":3,"audio_total_limit":0}]}}}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer api.Close()
	cfg := config.Load()
	cfg.BaseURL, cfg.AccessKey = api.URL, ""
	runner := newRootRunner(cfg)
	credentials := &marketingCredentialStore{credential: &auth.Credential{UID: "space-user", AccessKey: "space-test-key", ExpiredAt: time.Now().Add(time.Hour).Unix()}}
	runner.Auth = auth.NewManager(cfg, auth.WithCredentialStore(credentials))
	runner.SpaceStore = &space.Store{Dir: t.TempDir()}
	store, err := common.SpaceStoreForRunner(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(args ...string) (string, error) {
		var out, warnings bytes.Buffer
		root := newRootCommand(&out, &warnings, runner)
		root.SetIn(strings.NewReader(""))
		root.SetArgs(args)
		err := root.Execute()
		return out.String(), err
	}
	must := func(args ...string) string {
		t.Helper()
		out, err := execute(args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out
	}
	checkList := func(teamID string) {
		t.Helper()
		var result struct {
			Spaces  []listedSpace    `json:"spaces"`
			Current *space.Selection `json:"current"`
		}
		out := must("space", "list")
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		if result.Current == nil || result.Current.TeamID != teamID || len(result.Spaces) != 3 {
			t.Fatalf("unexpected space list: %s", out)
		}
		for _, item := range result.Spaces {
			if item.Current != (item.TeamID == teamID) {
				t.Fatalf("wrong current marker: %+v", item)
			}
		}
	}
	checkList("0")
	must("space", "use", "12")
	listTeamID = "12"
	checkList("12")
	file := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(file, []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	if out := must("upload-file", "--path", file); !strings.Contains(out, `"team_id":"12"`) {
		t.Fatal(out)
	}
	if out := must("submit-run", "--message", "create", "--asset-ids", "asset-a"); !strings.Contains(out, `"team_id":"12"`) {
		t.Fatal(out)
	}
	must("space", "use", "34")
	listTeamID = "34"
	checkList("34")
	must("model", "list", "--refresh")
	if requests[len(requests)-1] != config.GetAvailableModelListPath+":34" {
		t.Fatal("model query did not forward the selected team", requests)
	}
	must("model", "list", "--refresh", "--team-id", "0")
	if requests[len(requests)-1] != config.GetAvailableModelListPath+":" {
		t.Fatal("personal model query carried a team header", requests)
	}
	must("get-thread", "--thread-id", "thread-a", "--run-id", "run-a")
	if requests[len(requests)-1] != config.GetThreadPath+":12" {
		t.Fatal("old task used new default", requests)
	}
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"get-thread", "--thread-id", " thread-a ", "--run-id", " run-a "}, config.GetThreadPath},
		{[]string{"list-thread-file", "--thread-id", " thread-a "}, config.ListThreadFilePath},
		{[]string{"short-drama", "+submit-run", "--thread-id", " thread-a ", "--message", "continue"}, config.SubmitRunPath},
	} {
		must(tc.args...)
		if requests[len(requests)-1] != tc.path+":12" || requestRef.ThreadID != "thread-a" {
			t.Fatalf("normalized task used wrong space: %v, %+v", requests, requestRef)
		}
		if tc.path == config.GetThreadPath && requestRef.RunID != "run-a" {
			t.Fatalf("run ID was not normalized: %+v", requestRef)
		}
	}
	must("get-credit-balance")
	if requests[len(requests)-1] != config.GetCreditBalancePath+":34" {
		t.Fatal(requests)
	}
	must("get-credit-balance", "--team-id", "0")
	if requests[len(requests)-1] != config.GetCreditBalancePath+":" {
		t.Fatal(requests)
	}
	before := len(requests)
	for _, args := range [][]string{
		{"get-thread", "--thread-id", "thread-a", "--team-id", "34"},
		{"submit-run", "--message", "wrong asset", "--asset-ids", "asset-a"},
		{"get-credit-balance", "--team-id", "-1"},
		{"space", "use"},
		{"space", "use", "12", "34"},
		{"space", "current"},
		{"canvas", "get", "--asset-id", "canvas"},
	} {
		if _, err := execute(args...); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	if len(requests) != before {
		t.Fatal("invalid operations reached API")
	}
	if _, err := execute("space", "use", "99"); err == nil {
		t.Fatal("nonmember selection saved")
	}
	listFailure = true
	if _, err := execute("space", "use", "12"); err == nil {
		t.Fatal("list failure ignored")
	}
	if selected, err := store.Current(); err != nil || selected.TeamID != "34" {
		t.Fatalf("failed selection changed default: %+v, %v", selected, err)
	}
	before = len(requests)
	must("space", "use", "0")
	listTeamID = ""
	if len(requests) != before {
		t.Fatal("personal switch depends on team list")
	}
	if selected, err := store.Current(); err != nil || selected.TeamID != "0" {
		t.Fatalf("personal selection not saved: %+v, %v", selected, err)
	}
	listFailure = false
	checkList("0")
	before = len(requests)
	// Marketing preview resolves an existing thread before reading the new default.
	if err := store.SaveDefault(space.Selection{TeamID: "34", Name: "B"}); err != nil {
		t.Fatal(err)
	}
	requestFile := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(requestFile, []byte(`{"thread_id":"thread-a","message":"continue","general_agent_settings":{"video_model":"model"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	loadsBefore := credentials.loads
	if out := must("marketing", "generate", "--request", requestFile); !strings.Contains(out, `"team_id":"12"`) {
		t.Fatal(out)
	}
	if credentials.loads-loadsBefore != 2 {
		t.Fatal("preview must read identity at initialization and check it once")
	}
	if len(requests) != before {
		t.Fatal("preview contacted API")
	}
	if err := os.WriteFile(requestFile, []byte(`{"message":"new marketing task","general_agent_settings":{"video_model":"model"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	loadsBefore = credentials.loads
	if out := must("marketing", "generate", "--request", requestFile, "--execute"); !strings.Contains(out, `"team_id":"34"`) || !strings.Contains(out, `"space_context"`) {
		t.Fatal(out)
	}
	if credentials.loads-loadsBefore != 2 {
		t.Fatal("submission must read identity at initialization and check it only at the HTTP boundary")
	}
	if requests[len(requests)-1] != marketingPaths["generate"]+":34" {
		t.Fatal("independent marketing client lost scope", requests)
	}
	if err := store.SaveDefault(space.Selection{TeamID: "12", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	must("marketing", "query", "--thread-id", "marketing-thread", "--run-id", "marketing-run")
	if requests[len(requests)-1] != marketingPaths["query"]+":34" {
		t.Fatal("marketing task lost scope", requests)
	}
}

type spaceExternalAuthAdapter struct{ adapter.Default }

func (spaceExternalAuthAdapter) CLIOptions() adapter.CLIOptions {
	options := adapter.Default{}.CLIOptions()
	options.LocalCredentialsEnabled = false
	return options
}

func (spaceExternalAuthAdapter) PrepareAPIRequest(_ context.Context, req *http.Request) (adapter.AuthDecision, error) {
	req.Header.Set("Authorization", "Bearer adapter-key")
	return adapter.AuthProvided, nil
}

type spaceUnavailableAccount struct{ common.AuthManager }

func (spaceUnavailableAccount) SpaceAccount(context.Context) (string, error) {
	return "", errors.New("credential metadata unavailable")
}

func TestSpaceUsesAvailableIdentityInsteadOfLocalLoginSwitch(t *testing.T) {
	var requests []string
	denied := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer adapter-key" {
			t.Error("adapter authentication was not used")
		}
		requests = append(requests, r.URL.Path+":"+r.Header.Get("X-Team-Id"))
		switch r.URL.Path {
		case config.GetLiteTeamListPath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"team_info_list":[{"team_id":"12","team_name":"A"}],"has_more":false}}`))
		case config.GetCreditBalancePath:
			if denied {
				_, _ = w.Write([]byte(`{"ret":"101","errmsg":"denied"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ret":"0","data":{"total_remain_amount":"100"}}`))
		case config.UploadFilePath:
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(`{"ret":"0","data":{"pippit_asset_id":"asset-a"}}`))
		case config.SubmitRunPath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"run":{"thread_id":"thread-a","run_id":"run-a"}}}`))
		case config.GetThreadPath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"readable_text":"task result"}}`))
		case config.QueryGenerateVideoResultPath:
			_, _ = w.Write([]byte(`{"ret":"0","data":{"thread_id":"thread-a","run_id":"run-a","run_state":2}}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer api.Close()
	cfg := config.Load()
	// This AK is not used by the adapter and must not supply a false identity.
	cfg.BaseURL, cfg.AccessKey = api.URL, "unrelated-environment-key"
	policy := spaceExternalAuthAdapter{}
	runner := newRootRunnerWithAdapter(cfg, policy)
	dir := filepath.Join(t.TempDir(), "space-records")
	runner.SpaceStore = &space.Store{Dir: dir}
	execute := func(args ...string) (string, error) {
		var out bytes.Buffer
		root := newRootCommandWithAdapter(&out, io.Discard, runner, policy)
		root.SetArgs(args)
		err := root.Execute()
		return out.String(), err
	}
	out, err := execute("space", "list")
	if err != nil || !strings.Contains(out, `"team_id":"12"`) || !strings.Contains(out, `"current":null`) {
		t.Fatalf("space list without local identity = %s, %v", out, err)
	}
	if requests[len(requests)-1] != config.GetLiteTeamListPath+":" {
		t.Fatal("team list used a selected team", requests)
	}
	before := len(requests)
	for _, args := range [][]string{
		{"space", "use", "0"}, {"space", "use", "12"},
	} {
		if _, err := execute(args...); err == nil || !strings.Contains(err.Error(), "无法识别当前账号") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if len(requests) != before {
		t.Fatal("unknown identity tried to persist a default space")
	}
	for _, teamID := range []string{"0", "12", "34"} {
		if _, err := execute("get-credit-balance", "--team-id", teamID); err != nil {
			t.Fatal(err)
		}
		wantHeader := teamID
		if teamID == "0" {
			wantHeader = ""
		}
		if requests[len(requests)-1] != config.GetCreditBalancePath+":"+wantHeader {
			t.Fatal("explicit space was not forwarded", requests)
		}
	}
	file := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(file, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		path string
	}{
		{[]string{"upload-file", "--path", file}, config.UploadFilePath},
		{[]string{"submit-run", "--message", "test", "--asset-ids", "asset-a"}, config.SubmitRunPath},
		{[]string{"get-thread", "--thread-id", "thread-a"}, config.GetThreadPath},
		{[]string{"marketing", "query", "--thread-id", "thread-a", "--run-id", "run-a"}, config.QueryGenerateVideoResultPath},
	} {
		if _, err := execute(append(tc.args, "--team-id", "12")...); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if requests[len(requests)-1] != tc.path+":12" {
			t.Fatal("explicit task space was not forwarded", requests)
		}
	}
	before = len(requests)
	denied = true
	if _, err := execute("get-credit-balance", "--team-id", "12"); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("server denial was not returned: %v", err)
	}
	denied = false
	if len(requests) != before+1 || requests[len(requests)-1] != config.GetCreditBalancePath+":12" {
		t.Fatal("server denial retried or switched spaces", requests)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("unknown identity created local space records: %v", err)
	}
	if _, err := execute("get-credit-balance"); err != nil {
		t.Fatal("ordinary adapter requests must remain available", err)
	}
	// Metadata lookup failure must not block an explicitly scoped API request.
	cfg.AccessKey = ""
	runner.Auth = spaceUnavailableAccount{}
	if _, err := execute("get-credit-balance", "--team-id", "12"); err != nil {
		t.Fatal("local metadata failure blocked adapter authentication", err)
	}
	if requests[len(requests)-1] != config.GetCreditBalancePath+":12" {
		t.Fatal("metadata failure dropped explicit space", requests)
	}
	if _, err := execute("get-credit-balance"); err == nil {
		t.Fatal("implicit space silently ignored metadata failure")
	}
	// With a known matching account, the login-command switch must not suppress
	// saved defaults or their use by business requests.
	cfg.AccessKey = "adapter-key"
	runner.Auth = auth.NewManager(cfg, auth.WithCredentialStore(&marketingCredentialStore{credential: &auth.Credential{UID: "adapter-user", AccessKey: "adapter-key"}}))
	if _, err := execute("space", "use", "12"); err != nil {
		t.Fatal(err)
	}
	out, err = execute("space", "list")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Current *space.Selection `json:"current"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Current == nil || result.Current.TeamID != "12" {
		t.Fatalf("space list = %s, %v", out, err)
	}
	if _, err := execute("get-credit-balance"); err != nil {
		t.Fatal(err)
	}
	if requests[len(requests)-1] != config.GetCreditBalancePath+":12" {
		t.Fatal("saved default was ignored", requests)
	}
	if _, err := execute("get-credit-balance", "--team-id", "0"); err != nil {
		t.Fatal(err)
	}
	if requests[len(requests)-1] != config.GetCreditBalancePath+":" {
		t.Fatal("personal override was ignored", requests)
	}
}
