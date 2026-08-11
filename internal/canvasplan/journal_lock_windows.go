//go:build windows

package canvasplan

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

func fileInfoIsLinkLike(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func openFileNoFollow(path string, flags int, _ os.FileMode) (*os.File, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access := uint32(windows.GENERIC_READ)
	if flags&os.O_WRONLY != 0 {
		access = windows.GENERIC_WRITE
	} else if flags&os.O_RDWR != 0 {
		access = windows.GENERIC_READ | windows.GENERIC_WRITE
	}
	creation := uint32(windows.OPEN_EXISTING)
	switch {
	case flags&os.O_CREATE != 0 && flags&os.O_EXCL != 0:
		creation = windows.CREATE_NEW
	case flags&os.O_CREATE != 0 && flags&os.O_TRUNC != 0:
		creation = windows.CREATE_ALWAYS
	case flags&os.O_CREATE != 0:
		creation = windows.OPEN_ALWAYS
	case flags&os.O_TRUNC != 0:
		creation = windows.TRUNCATE_EXISTING
	}
	handle, err := windows.CreateFile(
		pathPtr,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		creation,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("refusing Windows reparse point: %s", path)
	}
	return os.NewFile(uintptr(handle), path), nil
}

func trustedSystemAncestorSymlink(string, os.FileInfo) bool {
	return false
}

func lockJournalFile(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&overlapped,
	)
}

func unlockJournalFile(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &overlapped)
}
