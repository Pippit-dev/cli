package canvasplan

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxJournalBytes = 16 << 20

type journalLock struct {
	path string
	file *os.File
}

func acquireJournalLock(path string) (*journalLock, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve CanvasPlan journal lock path: %w", err)
	}
	directory, err := ensureSecureJournalDirectory(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	file, err := openRegularFileNoFollow(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open CanvasPlan journal lock: %w", err)
	}
	closeOnError := func(cause error) (*journalLock, error) {
		_ = file.Close()
		return nil, cause
	}
	if err := directory.validateStable(); err != nil {
		return closeOnError(err)
	}
	if err := lockJournalFile(file); err != nil {
		return closeOnError(fmt.Errorf("CanvasPlan journal is locked: %s: %w", lockPath, err))
	}
	if err := file.Chmod(0o600); err != nil {
		_ = unlockJournalFile(file)
		return closeOnError(fmt.Errorf("secure CanvasPlan journal lock: %w", err))
	}
	if err := file.Truncate(0); err != nil {
		_ = unlockJournalFile(file)
		return closeOnError(fmt.Errorf("truncate CanvasPlan journal lock: %w", err))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = unlockJournalFile(file)
		return closeOnError(fmt.Errorf("seek CanvasPlan journal lock: %w", err))
	}
	if _, err := fmt.Fprintf(file, "pid=%d acquired_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		_ = unlockJournalFile(file)
		return closeOnError(fmt.Errorf("write CanvasPlan journal lock: %w", err))
	}
	if err := directory.validateStable(); err != nil {
		_ = unlockJournalFile(file)
		return closeOnError(err)
	}
	return &journalLock{path: lockPath, file: file}, nil
}

func (lock *journalLock) release() error {
	if lock == nil {
		return nil
	}
	if lock.file != nil {
		unlockErr := unlockJournalFile(lock.file)
		closeErr := lock.file.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}
	return nil
}

func loadOrCreateJournal(path, planHash, resolvedHash string, allowCreate bool) (*Journal, bool, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, false, fmt.Errorf("resolve CanvasPlan journal path: %w", err)
	}
	directory, err := ensureSecureJournalDirectory(filepath.Dir(path))
	if err != nil {
		return nil, false, err
	}
	file, err := openRegularFileNoFollow(path, os.O_RDWR, 0)
	if err == nil {
		defer file.Close()
		if err := file.Chmod(0o600); err != nil {
			return nil, false, fmt.Errorf("secure CanvasPlan journal permissions: %w", err)
		}
		journal, err := decodeJournal(io.LimitReader(file, maxJournalBytes+1))
		if err != nil {
			return nil, false, err
		}
		if err := validateJournal(journal, planHash, resolvedHash); err != nil {
			return nil, false, err
		}
		if err := directory.validateStable(); err != nil {
			return nil, false, err
		}
		return journal, false, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("open CanvasPlan journal: %w", err)
	}
	if !allowCreate {
		return nil, false, fmt.Errorf("CanvasPlan journal does not exist: %s", path)
	}
	operationID, err := randomOperationID()
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	journal := &Journal{
		Schema:              JournalSchema,
		OperationID:         operationID,
		RequestID:           "pippit_canvas_plan_" + operationID,
		PlanSHA256:          planHash,
		ResolvedMediaSHA256: resolvedHash,
		State:               StateInitialized,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := saveJournal(path, journal); err != nil {
		return nil, false, err
	}
	return journal, true, nil
}

func decodeJournal(reader io.Reader) (*Journal, error) {
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read CanvasPlan journal: %w", err)
	}
	if len(payload) > maxJournalBytes {
		return nil, fmt.Errorf("CanvasPlan journal exceeds %d bytes", maxJournalBytes)
	}
	var journal Journal
	if err := json.Unmarshal(payload, &journal); err != nil {
		return nil, fmt.Errorf("decode CanvasPlan journal: %w", err)
	}
	return &journal, nil
}

func validateJournal(journal *Journal, planHash, resolvedHash string) error {
	if journal == nil || journal.Schema != JournalSchema {
		return fmt.Errorf("unsupported CanvasPlan journal schema %q", valueOrEmpty(journal, func(j *Journal) string { return j.Schema }))
	}
	if strings.TrimSpace(journal.OperationID) == "" || strings.TrimSpace(journal.RequestID) == "" || strings.TrimSpace(journal.State) == "" {
		return fmt.Errorf("CanvasPlan journal identity or state is incomplete")
	}
	if journal.PlanSHA256 != planHash || journal.ResolvedMediaSHA256 != resolvedHash {
		return fmt.Errorf("CanvasPlan or resolved media changed after journal creation")
	}
	return nil
}

func saveJournal(path string, journal *Journal) error {
	if journal == nil {
		return fmt.Errorf("CanvasPlan journal is required")
	}
	journal.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	payload, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CanvasPlan journal: %w", err)
	}
	payload = append(payload, '\n')
	path, err = filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve CanvasPlan journal path: %w", err)
	}
	directory, err := ensureSecureJournalDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	destinationBefore, err := lstatRegularOrMissing(path)
	if err != nil {
		return fmt.Errorf("inspect CanvasPlan journal destination: %w", err)
	}
	temporary, err := os.CreateTemp(directory.path, ".canvas-plan-journal-*")
	if err != nil {
		return fmt.Errorf("create temporary CanvasPlan journal: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	temporaryInfo, err := os.Lstat(temporaryPath)
	if err != nil {
		return fmt.Errorf("inspect temporary CanvasPlan journal: %w", err)
	}
	if err := validateOpenedRegularFile(temporaryPath, temporary, temporaryInfo); err != nil {
		return fmt.Errorf("validate temporary CanvasPlan journal: %w", err)
	}
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary CanvasPlan journal: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		return fmt.Errorf("write temporary CanvasPlan journal: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary CanvasPlan journal: %w", err)
	}
	if err := directory.validateStable(); err != nil {
		return err
	}
	if err := validateDestinationUnchanged(path, destinationBefore); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary CanvasPlan journal: %w", err)
	}
	if err := directory.validateStable(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace CanvasPlan journal: %w", err)
	}
	replaced, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect replaced CanvasPlan journal: %w", err)
	}
	if fileInfoIsLinkLike(replaced) || !replaced.Mode().IsRegular() || !os.SameFile(temporaryInfo, replaced) {
		return fmt.Errorf("replaced CanvasPlan journal is not the secured temporary file")
	}
	if err := directory.validateStable(); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func validateDestinationUnchanged(path string, before os.FileInfo) error {
	current, err := lstatRegularOrMissing(path)
	if err != nil {
		return fmt.Errorf("reinspect CanvasPlan journal destination: %w", err)
	}
	if before == nil && current == nil {
		return nil
	}
	if before == nil || current == nil || !os.SameFile(before, current) {
		return fmt.Errorf("CanvasPlan journal destination changed during save")
	}
	return nil
}

func randomOperationID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate CanvasPlan operation ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func valueOrEmpty[T any](value *T, getter func(*T) string) string {
	if value == nil {
		return ""
	}
	return getter(value)
}
