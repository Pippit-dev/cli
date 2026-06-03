package updatecmd

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallSkillsInstallsAllBundledSkills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell script to stub npx")
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	capturePath := filepath.Join(t.TempDir(), "args.txt")
	npxPath := filepath.Join(binDir, "npx")
	script := "#!/bin/sh\nfor arg in \"$@\"; do printf '%s\\n' \"$arg\"; done > \"$CAPTURE_ARGS\"\n"
	if err := os.WriteFile(npxPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE_ARGS", capturePath)
	t.Setenv("HOME", t.TempDir())

	var stderr bytes.Buffer
	if err := installSkills(root, &stderr); err != nil {
		t.Fatalf("installSkills() error = %v, stderr = %s", err, stderr.String())
	}

	gotBytes, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(gotBytes)), "\n")
	want := []string{"-y", "skills", "add", root, "-g", "-y", "--skill", "*"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("npx args mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestCleanupLegacyGlobalSkills(t *testing.T) {
	globalSkillsDir := t.TempDir()
	for _, skillName := range []string{"pippit-short-drama-skill", "xyq-nest-skill", "xyq-short-drama-skill", "xyq-skill"} {
		if err := os.Mkdir(filepath.Join(globalSkillsDir, skillName), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := cleanupLegacyGlobalSkills(globalSkillsDir); err != nil {
		t.Fatalf("cleanupLegacyGlobalSkills() error = %v", err)
	}

	for _, skillName := range []string{"pippit-short-drama-skill", "xyq-nest-skill"} {
		if _, err := os.Stat(filepath.Join(globalSkillsDir, skillName)); !os.IsNotExist(err) {
			t.Fatalf("legacy skill %s still exists or stat failed: %v", skillName, err)
		}
	}
	for _, skillName := range []string{"xyq-short-drama-skill", "xyq-skill"} {
		if _, err := os.Stat(filepath.Join(globalSkillsDir, skillName)); err != nil {
			t.Fatalf("new skill %s was not preserved: %v", skillName, err)
		}
	}
}
