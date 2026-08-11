package canvasplan

import (
	"fmt"
	"os"
	"path/filepath"
)

// PreflightJournalPath applies the same no-follow directory, journal, and lock
// checks used by Execute before a caller performs any remote side effects.
func PreflightJournalPath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve CanvasPlan journal preflight path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if _, err := ensureSecureJournalDirectory(filepath.Dir(absolute)); err != nil {
		return fmt.Errorf("preflight CanvasPlan journal directory: %w", err)
	}

	info, err := lstatRegularOrMissing(absolute)
	if err != nil {
		return fmt.Errorf("inspect CanvasPlan journal during preflight: %w", err)
	}
	if info != nil {
		file, openErr := openRegularFileNoFollow(absolute, os.O_RDWR, 0)
		if openErr != nil {
			return fmt.Errorf("open CanvasPlan journal during preflight: %w", openErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("close CanvasPlan journal after preflight: %w", closeErr)
		}
	}

	lock, err := acquireJournalLock(absolute)
	if err != nil {
		return fmt.Errorf("preflight CanvasPlan journal lock: %w", err)
	}
	if err := lock.release(); err != nil {
		return fmt.Errorf("release CanvasPlan journal preflight lock: %w", err)
	}
	return nil
}
