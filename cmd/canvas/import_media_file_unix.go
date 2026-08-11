//go:build !windows

package canvas

import (
	"fmt"
	"os"
	"syscall"
)

func fileInfoIsImportMediaLinkLike(info os.FileInfo) bool {
	return info != nil && info.Mode()&os.ModeSymlink != 0
}

func openImportMediaNoFollow(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fileInfoIsImportMediaLinkLike(before) || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("import media must be a regular non-symbolic file: %s", path)
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	current, err := os.Lstat(path)
	if err != nil || fileInfoIsImportMediaLinkLike(current) || !current.Mode().IsRegular() ||
		!opened.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(opened, current) {
		_ = file.Close()
		return nil, fmt.Errorf("import media path changed while it was opened: %s", path)
	}
	return file, nil
}
