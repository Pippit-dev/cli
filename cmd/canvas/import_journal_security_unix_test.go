//go:build !windows

package canvas

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportCommandRejectsJournalAncestorSymlinkBeforeSideEffects(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(realParent, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(root, "linked")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}

	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &fakeImportMediaAPI{}
	executor := &fakeImportExecutor{}
	deps := testImportDependencies(root, exporter, media, executor)
	cmd := newImportCommand(io.Discard, io.Discard, deps)
	cmd.SetArgs([]string{
		"--from", "libtv",
		"--url", testLibTVURL,
		"--journal", filepath.Join(linkedParent, "nested", "import.journal.json"),
	})
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "symbolic link") {
		t.Fatalf("Execute() error = %v, want ancestor symbolic-link rejection", err)
	}
	if len(exporter.urls) != 0 || media.uploads != 0 || executor.calls != 0 {
		t.Fatalf(
			"export/upload/execute side effects = %d/%d/%d, want 0/0/0",
			len(exporter.urls), media.uploads, executor.calls,
		)
	}
}

func TestImportCommandChecksAutomaticJournalBeforeRemoteMediaUpload(t *testing.T) {
	root := t.TempDir()
	realConfig := filepath.Join(root, "real-config")
	if err := os.MkdirAll(realConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedConfig := filepath.Join(root, "linked-config")
	if err := os.Symlink(realConfig, linkedConfig); err != nil {
		t.Fatal(err)
	}

	plan, mediaBytes := testImportPlan(t, false)
	exporter := &fakeImportExporter{plan: plan, mediaBytes: mediaBytes}
	media := &fakeImportMediaAPI{}
	executor := &fakeImportExecutor{}
	deps := testImportDependencies(root, exporter, media, executor)
	deps.userConfigDir = func() (string, error) { return linkedConfig, nil }
	cmd := newImportCommand(io.Discard, io.Discard, deps)
	cmd.SetArgs([]string{"--from", "libtv", "--url", testLibTVURL})
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "symbolic link") {
		t.Fatalf("Execute() error = %v, want automatic-journal symbolic-link rejection", err)
	}
	if len(exporter.urls) != 1 || media.uploads != 0 || executor.calls != 0 {
		t.Fatalf(
			"export/upload/execute side effects = %d/%d/%d, want local export only (1/0/0)",
			len(exporter.urls), media.uploads, executor.calls,
		)
	}
	if len(exporter.bundles) != 1 {
		t.Fatalf("export bundles = %#v, want one local export", exporter.bundles)
	}
	if _, statErr := os.Stat(exporter.bundles[0]); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe automatic-journal export bundle was not removed: %v", statErr)
	}
}
