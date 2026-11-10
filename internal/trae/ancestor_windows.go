package trae

import (
	"context"
	"unsafe"

	"golang.org/x/sys/windows"
)

func parentExecutable(_ context.Context, pid int) (int, string) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, ""
	}
	defer windows.CloseHandle(h)
	var info windows.PROCESS_BASIC_INFORMATION
	if err := windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&info), uint32(unsafe.Sizeof(info)), nil); err != nil {
		return 0, ""
	}
	buf := make([]uint16, 4096)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return int(info.InheritedFromUniqueProcessId), ""
	}
	return int(info.InheritedFromUniqueProcessId), windows.UTF16ToString(buf[:size])
}
