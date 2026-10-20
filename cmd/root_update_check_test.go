package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/version"
	"github.com/spf13/cobra"
)

type noSelfUpdateAdapter struct{ adapter.Default }

func (noSelfUpdateAdapter) CLIOptions() adapter.CLIOptions {
	options := adapter.Default{}.CLIOptions()
	options.SelfUpdateEnabled = false
	return options
}

func setupRootUpdateCache(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, key := range []string{"CI", "PIPPIT_CLI_DISABLE_UPDATE_CHECK", "PIPPIT_CLI_UPDATE_CHECKED"} {
		t.Setenv(key, "")
	}
	previous := version.Version
	version.Version = "1.0.41"
	t.Cleanup(func() { version.Version = previous })
	cacheFile := filepath.Join(home, ".pippit_tool_cli", "version-check.json")
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0700); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf(`{"latest":"1.0.42","checkedAt":%d}`, time.Now().UnixMilli())
	if err := os.WriteFile(cacheFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return cacheFile
}

func TestNativeBusinessInvocationReportsUpdateBeforeUnchangedJSON(t *testing.T) {
	setupRootUpdateCache(t)
	for _, commandName := range []string{"probe", "login"} {
		for range 2 {
			var stdout, stderr bytes.Buffer
			root := NewRootCommand(&stdout, &stderr)
			probe := &cobra.Command{Use: "probe"}
			if commandName == "login" {
				probe, _, _ = root.Find([]string{"login"})
			} else {
				root.AddCommand(probe)
			}
			probe.RunE = func(*cobra.Command, []string) error {
				if !strings.Contains(stderr.String(), "1.0.41 -> 1.0.42") {
					t.Fatal("business operation started before the synchronous notice")
				}
				_, err := fmt.Fprintln(&stdout, `{"ok":true}`)
				return err
			}
			root.SetArgs([]string{commandName})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !json.Valid(stdout.Bytes()) || stdout.String() != "{\"ok\":true}\n" ||
				strings.Count(stderr.String(), "New version available") != 1 {
				t.Fatalf("stdout/stderr contract changed: %q / %q", stdout.String(), stderr.String())
			}
			if (commandName == "login") != strings.Contains(stderr.String(), "authorization fails") {
				t.Fatal("login-specific notice was omitted or leaked into another command")
			}
		}
	}
}

func TestNativeUpdateCheckRespectsAdapterAndInvocationBoundaries(t *testing.T) {
	setupRootUpdateCache(t)
	for _, scenario := range []string{"help", "version", "empty", "invalid", "invalid flag", "update", "adapter", "dev", "CI", "PIPPIT_CLI_DISABLE_UPDATE_CHECK", "PIPPIT_CLI_UPDATE_CHECKED"} {
		t.Run(scenario, func(t *testing.T) {
			policy := adapter.Adapter(adapter.Default{})
			args := []string{"probe"}
			switch scenario {
			case "help":
				args = []string{"probe", "--help"}
			case "version":
				args = []string{"--version"}
			case "empty":
				args = []string{}
			case "invalid":
				args = []string{"unknown-command"}
			case "invalid flag":
				args = []string{"probe", "--unknown"}
			case "update":
				args = []string{"update"}
			case "adapter":
				policy = noSelfUpdateAdapter{}
			case "dev":
				previous := version.Version
				version.Version = "dev"
				t.Cleanup(func() { version.Version = previous })
			default:
				t.Setenv(scenario, "1")
			}
			var stdout, stderr bytes.Buffer
			root := NewRootCommandWithAdapter(&stdout, &stderr, policy)
			root.AddCommand(&cobra.Command{Use: "probe", Run: func(*cobra.Command, []string) {}})
			if scenario == "update" {
				update, _, _ := root.Find([]string{"update"})
				update.RunE = func(*cobra.Command, []string) error { return nil }
			}
			root.SetArgs(args)
			err := root.Execute()
			if (scenario == "invalid" || scenario == "invalid flag") != (err != nil) {
				t.Fatalf("unexpected command result: %v", err)
			}
			if strings.Contains(stderr.String(), "New version available") {
				t.Fatalf("skipped invocation emitted an update notice: %s", stderr.String())
			}
		})
	}
}

func TestConstructingRootHasNoVersionCheckSideEffects(t *testing.T) {
	cacheFile := setupRootUpdateCache(t)
	if err := os.Remove(cacheFile); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	_ = NewRootCommand(&stdout, &stderr)
	if _, err := os.Stat(cacheFile); !os.IsNotExist(err) || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal("constructing the root queried versions, wrote cache or produced output")
	}
}
