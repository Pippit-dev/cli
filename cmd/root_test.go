package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

func TestRootRunnerReadsUpdatedAccessKeyForEveryRequest(t *testing.T) {
	received := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received = append(received, request.Header.Get("Authorization"))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := config.Load()
	cfg.BaseURL = server.URL
	cfg.HTTPTimeout = time.Second
	cfg.AccessKey = "first-key"
	runner := newRootRunner(cfg)

	for _, accessKey := range []string{"first-key", "second-key"} {
		runner.Config.AccessKey = accessKey
		var response map[string]any
		if err := runner.Client.SendRequest(context.Background(), "/probe", map[string]any{}, &response); err != nil {
			t.Fatalf("SendRequest(%q) error = %v", accessKey, err)
		}
	}
	if got, want := strings.Join(received, ","), "Bearer first-key,Bearer second-key"; got != want {
		t.Fatalf("Authorization headers = %q, want %q", got, want)
	}
}

func TestRootRegistersCanvas(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	command, _, err := root.Find([]string{"canvas"})
	if err != nil || command == nil || command.Name() != "canvas" {
		t.Fatalf("root.Find(%q) = %#v, %v", "canvas", command, err)
	}
}

func TestRootRegistersGetCreditBalance(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	command, _, err := root.Find([]string{"get-credit-balance"})
	if err != nil || command == nil || command.Name() != "get-credit-balance" {
		t.Fatalf("root.Find(%q) = %#v, %v", "get-credit-balance", command, err)
	}
}

func TestRootRegistersTopLevelBrowserAuthCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, name := range []string{"login", "status", "logout"} {
		command, _, err := root.Find([]string{name})
		if err != nil || command == nil || command.Name() != name {
			t.Fatalf("root.Find(%q) = %#v, %v", name, command, err)
		}
	}
}

func TestHelpDiscoversNpmCanvasCommands(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"canvas", "--help"}, {"canvas", "command", "--help"}, {"canvas", "command"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := NewRootCommand(&stdout, &stderr)
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			for _, action := range []string{"list", "describe", "schema", "guide", "run"} {
				if !strings.Contains(stdout.String(), "canvas command "+action) {
					t.Fatalf("help does not expose %s: %s", action, stdout.String())
				}
			}
			if !strings.Contains(stdout.String(), "npm launcher") {
				t.Fatalf("help does not explain launcher boundary: %s", stdout.String())
			}
		})
	}
}

func TestNativeCanvasCommandsRequireNpmLauncher(t *testing.T) {
	for _, action := range []string{"list", "describe", "schema", "guide", "run", "unknown-action"} {
		t.Run(action, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := NewRootCommand(&stdout, &stderr)
			root.SetArgs([]string{"canvas", "command", action, "--category", "example"})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "npm launcher") {
				t.Fatalf("Execute() error = %v, want launcher guidance", err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, must not claim execution success", stdout.String())
			}
		})
	}
}
