//go:build darwin

package canvas

import (
	"os"

	"golang.org/x/sys/unix"
)

func importFileIsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	_, err := unix.IoctlGetTermios(int(file.Fd()), uint(unix.TIOCGETA))
	return err == nil
}
