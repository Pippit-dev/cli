package trae

import (
	"context"
	"os"
	"strconv"
	"strings"
)

func parentExecutable(_ context.Context, pid int) (int, string) {
	dir := "/proc/" + strconv.Itoa(pid)
	stat, err := os.ReadFile(dir + "/stat")
	if err != nil {
		return 0, ""
	}
	line := string(stat)
	end := strings.LastIndexByte(line, ')')
	if end < 0 {
		return 0, ""
	}
	fields := strings.Fields(line[end+1:])
	if len(fields) < 2 {
		return 0, ""
	}
	parent, _ := strconv.Atoi(fields[1])
	// The kernel keeps a launch name even when /proc/PID/exe resolves a symlink.
	start := strings.IndexByte(line, '(')
	comm := ""
	if start >= 0 && start < end {
		comm = line[start+1 : end]
	}
	if strings.EqualFold(comm, "traex") || strings.EqualFold(comm, "trae cn") {
		return parent, comm
	}
	exe, err := os.Readlink(dir + "/exe")
	if err != nil {
		exe = comm
	}
	return parent, exe
}
