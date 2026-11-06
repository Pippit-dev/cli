package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type ipCredentialStore struct {
	auth.CredentialStore
	loads int
}

func (store *ipCredentialStore) Load(context.Context) (*auth.Credential, error) {
	store.loads++
	return nil, errors.New("corrupt credential fixture")
}

type ipCommandPolicy struct {
	adapter.Default
	options adapter.CLIOptions
}

func (policy ipCommandPolicy) CLIOptions() adapter.CLIOptions { return policy.options }

func TestIPCommandDoesNotReadCredentialsOrInitializeSpace(t *testing.T) {
	for _, accessKey := range []string{"", "existing-key"} {
		t.Run("configured_key="+accessKey, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, header := range []string{"Authorization", "Cookie", "X-Team-Id"} {
					if r.Header.Get(header) != "" {
						t.Errorf("anonymous command sent %s", header)
					}
				}
				_, _ = io.WriteString(w, `{"ret":"0","data":{"client_ip":"8.8.8.8"}}`)
			}))
			defer server.Close()
			cfg := config.Load()
			cfg.BaseURL, cfg.AccessKey = server.URL, accessKey
			cfg.HTTPTimeout = time.Second
			policy := ipCommandPolicy{options: adapter.CLIOptions{
				DefaultAllowRootCommands: true,
				LocalCredentialsEnabled:  true,
			}}
			runner := newRootRunnerWithAdapter(cfg, policy)
			store := &ipCredentialStore{}
			runner.Auth = auth.NewManager(cfg, auth.WithCredentialStore(store))
			var stdout, stderr bytes.Buffer
			root := newRootCommandWithAdapter(&stdout, &stderr, runner, policy)
			root.SetArgs([]string{"--team-id", "42", "ip"})
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute() = %v, stderr = %q", err, stderr.String())
			}
			if store.loads != 0 {
				t.Fatalf("credential loads = %d, want 0 even with corrupt credentials", store.loads)
			}
			command, _, err := root.Find([]string{"ip"})
			if err != nil || command == nil || common.SpaceSession(command.Context()) != nil {
				t.Fatalf("ip initialized a space session: %v", err)
			}
			var output map[string]string
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil || len(output) != 1 || output["client_ip"] != "8.8.8.8" {
				t.Fatalf("stdout = %q, error = %v, want only client_ip", stdout.String(), err)
			}
		})
	}
}

func TestIPCommandRespectsAdapterFiltering(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		options := adapter.CLIOptions{}
		if allowed {
			options.AllowedRootCommands = []string{"ip"}
		}
		root := NewRootCommandWithAdapter(io.Discard, io.Discard, ipCommandPolicy{options: options})
		command, _, _ := root.Find([]string{"ip"})
		if got := command != nil && command.Name() == "ip"; got != allowed {
			t.Fatalf("ip present = %v, want %v", got, allowed)
		}
	}
}

func TestIPCommandRejectsArguments(t *testing.T) {
	command := newIPCommand(io.Discard, io.Discard, nil, adapter.Default{})
	command.SetArgs([]string{"unexpected"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("Execute() = %v, want positional argument rejection", err)
	}
}
