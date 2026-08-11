package canvas

import (
	"fmt"
	"os"
	"path/filepath"
)

type importMediaCheckpointLock struct {
	file   *os.File
	unlock func() error
}

func acquireImportMediaCheckpointLock(path string) (*importMediaCheckpointLock, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create canvas import media lock directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("canvas import media checkpoint lock must not be a symbolic link")
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("canvas import media checkpoint lock must be a regular file")
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect canvas import media checkpoint lock: %w", err)
	}
	file, err := openImportMediaLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("open canvas import media checkpoint lock: %w", err)
	}
	fileInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect opened canvas import media checkpoint lock: %w", err)
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("reinspect canvas import media checkpoint lock: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("canvas import media checkpoint lock must not be a symbolic link")
	}
	if !fileInfo.Mode().IsRegular() || !pathInfo.Mode().IsRegular() || !os.SameFile(fileInfo, pathInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("canvas import media checkpoint lock changed while it was being opened")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure canvas import media checkpoint lock: %w", err)
	}
	unlock, err := lockImportMediaFile(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("canvas import media checkpoint is locked by another process: %w", err)
	}
	return &importMediaCheckpointLock{file: file, unlock: unlock}, nil
}

func (lock *importMediaCheckpointLock) release() error {
	if lock == nil {
		return nil
	}
	var unlockErr error
	if lock.unlock != nil {
		unlockErr = lock.unlock()
	}
	if lock.file != nil {
		if closeErr := lock.file.Close(); unlockErr == nil {
			unlockErr = closeErr
		}
	}
	return unlockErr
}
