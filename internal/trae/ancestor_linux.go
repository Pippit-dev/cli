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
	exe, err := os.Readlink(dir + "/exe")
	if err != nil {
		if start := strings.IndexByte(line, '('); start >= 0 && start < end {
			exe = line[start+1 : end]
		}
	}
	return parent, exe
}
