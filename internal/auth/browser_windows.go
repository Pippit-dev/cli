//go:build windows

package auth

import (
	"os"
	"os/exec"
)

func OpenBrowser(rawURL string) error {
	command := exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	command.Env = SanitizedBrowserEnv(os.Environ())
	return command.Start()
}
