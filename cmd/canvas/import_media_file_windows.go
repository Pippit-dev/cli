//go:build windows

package canvas

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

func fileInfoIsImportMediaLinkLike(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func openImportMediaNoFollow(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fileInfoIsImportMediaLinkLike(before) || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("import media must be a regular non-reparse file: %s", path)
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	var handleInfo windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &handleInfo); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	if handleInfo.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("import media must not be a Windows reparse point: %s", path)
	}
	file := os.NewFile(uintptr(handle), path)
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
