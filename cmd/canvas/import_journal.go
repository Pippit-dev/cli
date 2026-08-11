package canvas

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
)

func preflightExplicitImportJournal(value string, explicitlySet bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !explicitlySet && trimmed == "" {
		return "", nil
	}
	if trimmed == "" {
		return "", fmt.Errorf(
			"canvas import --journal was explicitly set but is empty; unset/omit --journal to use the automatic journal, or first mkdir a dedicated writable directory and pass a file inside it",
		)
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve canvas import --journal: %w", err)
	}
	absolute = filepath.Clean(absolute)
	parent := filepath.Dir(absolute)
	if absolute == parent || filepath.Dir(parent) == parent {
		return "", fmt.Errorf(
			"canvas import --journal %q resolves at or directly under the filesystem root; this often means a shell directory variable was empty; unset/omit --journal to use the automatic journal, or first mkdir a dedicated writable directory and pass a file inside it",
			absolute,
		)
	}
	if info, lstatErr := os.Lstat(absolute); lstatErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("canvas import --journal must not be a symbolic link: %s", absolute)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("canvas import --journal must name a regular file: %s", absolute)
		}
	} else if !os.IsNotExist(lstatErr) {
		return "", fmt.Errorf("inspect canvas import --journal: %w", lstatErr)
	}
	parentInfo, err := os.Lstat(parent)
	if os.IsNotExist(err) {
		return "", fmt.Errorf(
			"canvas import --journal parent does not exist: %s; first run mkdir -p %q, or unset/omit --journal to use the automatic journal",
			parent, parent,
		)
	}
	if err != nil {
		return "", fmt.Errorf("inspect canvas import --journal parent: %w", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return "", fmt.Errorf("canvas import --journal parent must be a real directory: %s", parent)
	}
	if err := canvasplan.PreflightJournalPath(absolute); err != nil {
		return "", fmt.Errorf(
			"canvas import --journal path is not safe and writable: %s; choose or first mkdir a writable directory, or unset/omit --journal to use the automatic journal: %w",
			parent, err,
		)
	}
	return absolute, nil
}
