package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/spf13/cobra"
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

func TestPPEEnvFlagOverridesEnvironment(t *testing.T) {
	t.Setenv(config.EnvPPEEnv, "ppe_from_env")
	cfg, root, ran := newPPEFlagTestRoot(t)
	root.SetArgs([]string{"ppe-probe", "--ppe-env", "ppe_from_flag"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !*ran {
		t.Fatal("probe command did not run")
	}
	if cfg.PPEEnv != "ppe_from_flag" {
		t.Fatalf("PPEEnv = %q, want flag value", cfg.PPEEnv)
	}
}

func TestPPEEnvUsesEnvironmentByDefault(t *testing.T) {
	t.Setenv(config.EnvPPEEnv, " ppe_from_env ")
	cfg, root, ran := newPPEFlagTestRoot(t)
	root.SetArgs([]string{"ppe-probe"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !*ran {
		t.Fatal("probe command did not run")
	}
	if cfg.PPEEnv != "ppe_from_env" {
		t.Fatalf("PPEEnv = %q, want environment value", cfg.PPEEnv)
	}
}

func TestPPEEnvCanBeExplicitlyDisabledByFlag(t *testing.T) {
	t.Setenv(config.EnvPPEEnv, "ppe_from_env")
	cfg, root, ran := newPPEFlagTestRoot(t)
	root.SetArgs([]string{"ppe-probe", "--ppe-env", ""})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !*ran {
		t.Fatal("probe command did not run")
	}
	if cfg.PPEEnv != "" {
		t.Fatalf("PPEEnv = %q, want production", cfg.PPEEnv)
	}
}

func TestPPEEnvRejectsInvalidValueBeforeCommand(t *testing.T) {
	t.Setenv(config.EnvPPEEnv, "production")
	_, root, ran := newPPEFlagTestRoot(t)
	root.SetArgs([]string{"ppe-probe"})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "PPE 环境") {
		t.Fatalf("Execute() error = %v, want invalid PPE error", err)
	}
	if *ran {
		t.Fatal("probe command ran with invalid PPE environment")
	}
}

func TestRootHelpIncludesPPEFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "--ppe-env") {
		t.Fatalf("help does not include --ppe-env:\n%s", stdout.String())
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

func newPPEFlagTestRoot(t *testing.T) (*config.Config, *cobra.Command, *bool) {
	t.Helper()
	cfg := config.Load()
	runner := common.NewRunner(cfg, nil)
	root := newRootCommand(&bytes.Buffer{}, &bytes.Buffer{}, runner)
	ran := false
	root.AddCommand(&cobra.Command{
		Use: "ppe-probe",
		Run: func(_ *cobra.Command, _ []string) {
			ran = true
		},
	})
	return cfg, root, &ran
}
