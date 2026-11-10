package trae

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeTraeParent(t *testing.T) {
	if os.Getenv("TEST_TRAE_CHILD") == "1" {
		if !Ancestor() {
			t.Fatal("Trae parent was not recognized")
		}
		return
	}
	// Use a real parent executable and leave the shell alive until the child exits.
	shell, err := os.ReadFile("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(t.TempDir(), "traex")
	if err := os.WriteFile(parent, shell, 0700); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(parent, "-c", `"$1" -test.run='^TestNativeTraeParent$'; exit $?`, "sh", testBinary)
	cmd.Env = append(os.Environ(), "TEST_TRAE_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
}
