//go:build windows

package canvas

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func openImportMediaLockFile(path string) (*os.File, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("inspect lock file attributes: %w", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("canvas import media checkpoint lock must not be a symbolic link or reparse point")
	}
	return os.NewFile(uintptr(handle), path), nil
}

func lockImportMediaFile(file *os.File) (func() error, error) {
	overlapped := &windows.Overlapped{}
	handle := windows.Handle(file.Fd())
	if err := windows.LockFileEx(
		handle,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		overlapped,
	); err != nil {
		return nil, err
	}
	return func() error {
		return windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
	}, nil
}
