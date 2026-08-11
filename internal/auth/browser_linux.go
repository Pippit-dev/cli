//go:build linux

package auth

import (
	"os"
	"os/exec"
)

func OpenBrowser(rawURL string) error {
	command := exec.Command("xdg-open", rawURL)
	command.Env = SanitizedBrowserEnv(os.Environ())
	return command.Start()
}
