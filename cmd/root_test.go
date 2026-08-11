package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/spf13/cobra"
)

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
