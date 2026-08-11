//go:build darwin

package auth

import (
	"os"
	"os/exec"
)

func OpenBrowser(rawURL string) error {
	command := exec.Command("open", rawURL)
	command.Env = SanitizedBrowserEnv(os.Environ())
	return command.Start()
}
