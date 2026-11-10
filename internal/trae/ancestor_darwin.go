package trae

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

func parentExecutable(ctx context.Context, pid int) (int, string) {
	// comm contains the executable path, including spaces, without its arguments.
	out, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "ppid=,comm=").Output()
	if err != nil {
		return 0, ""
	}
	parent, executable, ok := strings.Cut(strings.TrimSpace(string(out)), " ")
	if !ok {
		return 0, ""
	}
	n, _ := strconv.Atoi(parent)
	return n, strings.TrimSpace(executable)
}
