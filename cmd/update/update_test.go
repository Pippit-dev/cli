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
