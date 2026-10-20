package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildVersionNeverInfersAReleaseFromWorkingDirectory(t *testing.T) {
	previous := Version
	t.Cleanup(func() { Version = previous })
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"@pippit-dev/cli","version":"99.0.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Error(err)
		}
	})
	for _, value := range []string{"", "dev", " dev "} {
		Version = value
		if got := BuildVersion(); got != "dev" {
			t.Fatalf("BuildVersion(%q) = %q", value, got)
		}
	}
	Version = " 1.0.42-canary.1 "
	if got := BuildVersion(); got != "1.0.42-canary.1" {
		t.Fatalf("release BuildVersion = %q", got)
	}
}
