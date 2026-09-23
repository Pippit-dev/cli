package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type marketingCredentialStore struct {
	auth.CredentialStore
	credential *auth.Credential
	loads      int
}

func (s *marketingCredentialStore) Load(context.Context) (*auth.Credential, error) {
	s.loads++
	if s.credential == nil {
		return nil, auth.ErrCredentialNotFound
	}
	return s.credential, nil
}

const marketingRequest = `{"message":"make an ad","general_agent_settings":{"video_model":"chosen-model","show_subtitle":false}}`

func TestMarketingUsesSharedBrowserAuth(t *testing.T) {
	for _, action := range []string{"generate", "query", "upload", "balance"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer browser-secret" {
					t.Error("browser login not reused")
				}
				if r.URL.Path != marketingPaths[action] || r.Method != "POST" {
					t.Error("wrong endpoint")
				}
				data, _ := io.ReadAll(r.Body)
				if bytes.Contains(data, []byte("browser-secret")) {
					t.Error("credential leaked into payload")
				}
				if action == "upload" && !bytes.Contains(data, []byte(`name="file"`)) {
					t.Error("missing multipart file")
				}
				fmt.Fprint(w, `{"ret":"0","log_id":"log-test","data":{"thread_id":"thread","run_id":"run"}}`)
			}))
			defer server.Close()
			cfg := config.Load()
			cfg.BaseURL = server.URL
			cfg.AccessKey = ""
			store := &marketingCredentialStore{credential: &auth.Credential{AccessKey: "browser-secret", UID: "user", DeviceID: "device", ExpiredAt: time.Now().Add(time.Hour).Unix()}}
			runner := newRootRunner(cfg)
			runner.Auth = auth.NewManager(cfg, auth.WithCredentialStore(store))
			var output bytes.Buffer
			root := newRootCommand(&output, io.Discard, runner)
			args := []string{"marketing", action}
			switch action {
			case "generate":
				args = append(args, "--request", "-", "--execute")
				root.SetIn(strings.NewReader(marketingRequest))
			case "query":
				args = append(args, "--thread-id", "thread", "--run-id", "run")
			case "upload":
				file := filepath.Join(t.TempDir(), "product.png")
				if err := os.WriteFile(file, []byte("image"), 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--file", file)
			}
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || store.loads != 1 {
				t.Fatalf("HTTP calls=%d credential loads=%d", calls, store.loads)
			}
			if strings.Contains(output.String(), "browser-secret") || !strings.Contains(output.String(), "log-test") {
				t.Fatal("raw result or credential boundary broken")
			}
		})
	}
}

func TestMarketingAuthFailureAndEnvironmentPrecedence(t *testing.T) {
	for _, scenario := range []string{"missing", "expired", "override"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer explicit-key" {
					t.Error("environment override not preserved")
				}
				fmt.Fprint(w, `{"ret":"0","data":{}}`)
			}))
			defer server.Close()
			cfg := config.Load()
			cfg.BaseURL = server.URL
			cfg.AccessKey = ""
			store := &marketingCredentialStore{}
			if scenario == "expired" {
				store.credential = &auth.Credential{AccessKey: "expired", ExpiredAt: 1}
			}
			if scenario == "override" {
				cfg.AccessKey = "explicit-key"
			}
			runner := newRootRunner(cfg)
			runner.Auth = auth.NewManager(cfg, auth.WithCredentialStore(store))
			root := newRootCommand(io.Discard, io.Discard, runner)
			root.SetArgs([]string{"marketing", "balance"})
			err := root.Execute()
			if scenario == "override" {
				if err != nil || calls != 1 || store.loads != 0 {
					t.Fatalf("override: err=%v calls=%d loads=%d", err, calls, store.loads)
				}
			} else if err == nil || !strings.Contains(err.Error(), "pippit-tool-cli login") || calls != 0 {
				t.Fatalf("missing/expired: err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestMarketingPreviewAndHelpDoNotReadCredentials(t *testing.T) {
	for _, args := range [][]string{{"marketing", "--help"}, {"marketing", "generate", "--request", "-"}, {"marketing", "query"}} {
		cfg := config.Load()
		cfg.AccessKey = ""
		store := &marketingCredentialStore{}
		runner := newRootRunner(cfg)
		runner.Auth = auth.NewManager(cfg, auth.WithCredentialStore(store))
		root := newRootCommand(io.Discard, io.Discard, runner)
		root.SetIn(strings.NewReader(marketingRequest))
		root.SetArgs(args)
		err := root.Execute()
		if args[1] != "query" && err != nil {
			t.Fatal(err)
		}
		if args[1] == "query" && err == nil {
			t.Fatal("missing IDs accepted")
		}
		if store.loads != 0 {
			t.Fatal("offline operation accessed credentials")
		}
	}
}

func TestMarketingDoesNotReplaySubmission(t *testing.T) {
	for _, status := range []int{302, 307, 504} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(status)
			}))
			defer server.Close()
			cfg := config.Load()
			cfg.BaseURL = server.URL
			cfg.AccessKey = "test-key"
			root := newRootCommand(io.Discard, io.Discard, newRootRunner(cfg))
			root.SetIn(strings.NewReader(marketingRequest))
			root.SetArgs([]string{"marketing", "generate", "--request", "-", "--execute"})
			if err := root.Execute(); err == nil {
				t.Fatal("failed/redirected request succeeded")
			}
			if calls != 1 {
				t.Fatalf("submission replayed %d times", calls)
			}
		})
	}
}
