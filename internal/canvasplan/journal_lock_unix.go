//go:build !windows

package canvasplan

import (
	"os"
	"path/filepath"
	"syscall"
)

func openFileNoFollow(path string, flags int, mode os.FileMode) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func fileInfoIsLinkLike(info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeSymlink != 0
}

func trustedSystemAncestorSymlink(path string, info os.FileInfo) bool {
	parent := filepath.Dir(path)
	if filepath.Dir(parent) != parent {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}

func lockJournalFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockJournalFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
