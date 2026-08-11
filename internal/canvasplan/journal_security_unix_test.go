//go:build !windows

package canvasplan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutorRejectsSymlinkJournalWithoutTouchingTarget(t *testing.T) {
	directory := t.TempDir()
	victim := filepath.Join(directory, "victim.json")
	writeVictim(t, victim)
	journalPath := filepath.Join(directory, "journal.json")
	if err := os.Symlink(victim, journalPath); err != nil {
		t.Fatal(err)
	}

	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	result, err := (&Executor{api: api}).Execute(
		context.Background(),
		plan,
		resolved,
		ExecuteOptions{JournalPath: journalPath},
	)
	if err == nil || result != nil || !strings.Contains(strings.ToLower(err.Error()), "symbolic link") {
		t.Fatalf("Execute() result=%#v error=%v, want symlink rejection", result, err)
	}
	if api.totalCalls() != 0 {
		t.Fatalf("remote API was called %d times", api.totalCalls())
	}
	assertVictimUnchanged(t, victim)
}

func TestExecutorRejectsSymlinkLockWithoutTouchingTarget(t *testing.T) {
	directory := t.TempDir()
	victim := filepath.Join(directory, "lock-victim")
	writeVictim(t, victim)
	journalPath := filepath.Join(directory, "journal.json")
	if err := os.Symlink(victim, journalPath+".lock"); err != nil {
		t.Fatal(err)
	}

	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	result, err := (&Executor{api: api}).Execute(
		context.Background(),
		plan,
		resolved,
		ExecuteOptions{JournalPath: journalPath},
	)
	if err == nil || result != nil || !strings.Contains(strings.ToLower(err.Error()), "symbolic link") {
		t.Fatalf("Execute() result=%#v error=%v, want lock symlink rejection", result, err)
	}
	if api.totalCalls() != 0 {
		t.Fatalf("remote API was called %d times", api.totalCalls())
	}
	assertVictimUnchanged(t, victim)
}

func TestExecutorRejectsSymlinkParentDirectory(t *testing.T) {
	directory := t.TempDir()
	realParent := filepath.Join(directory, "real-parent")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(directory, "linked-parent")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(linkedParent, "journal.json")

	plan, resolved := testPlanAndResolved()
	api := newFakeCanvasAPI(len(plan.Nodes))
	result, err := (&Executor{api: api}).Execute(
		context.Background(),
		plan,
		resolved,
		ExecuteOptions{JournalPath: journalPath},
	)
	if err == nil || result != nil || !strings.Contains(strings.ToLower(err.Error()), "symbolic link") {
		t.Fatalf("Execute() result=%#v error=%v, want parent symlink rejection", result, err)
	}
	if api.totalCalls() != 0 {
		t.Fatalf("remote API was called %d times", api.totalCalls())
	}
	for _, unexpected := range []string{filepath.Join(realParent, "journal.json"), filepath.Join(realParent, "journal.json.lock")} {
		if _, statErr := os.Lstat(unexpected); !os.IsNotExist(statErr) {
			t.Fatalf("unexpected file through symlink %s: %v", unexpected, statErr)
		}
	}
}

func TestSaveJournalRejectsSymlinkDestinationWithoutTouchingTarget(t *testing.T) {
	directory := t.TempDir()
	victim := filepath.Join(directory, "save-victim.json")
	writeVictim(t, victim)
	journalPath := filepath.Join(directory, "journal.json")
	if err := os.Symlink(victim, journalPath); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Schema: JournalSchema, OperationID: "operation", RequestID: "request", State: StateInitialized}
	if err := saveJournal(journalPath, journal); err == nil || !strings.Contains(strings.ToLower(err.Error()), "symbolic link") {
		t.Fatalf("saveJournal() error = %v, want symlink rejection", err)
	}
	assertVictimUnchanged(t, victim)
}

func writeVictim(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("do-not-touch\n"), 0o640); err != nil {
		t.Fatal(err)
	}
}

func assertVictimUnchanged(t *testing.T, path string) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "do-not-touch\n" {
		t.Fatalf("victim content changed: %q", payload)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len(payload)) {
		t.Fatalf("victim size changed: %d", info.Size())
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("victim mode changed: %o", info.Mode().Perm())
	}
}
