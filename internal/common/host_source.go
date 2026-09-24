package common

import (
	"os"
	"strings"
)

// DetectHostSource uses explicit environment attribution before runtime markers.
// It never reads credentials, session contents, process arguments or installed apps.
func DetectHostSource() string {
	return detectHostSource(os.Getenv)
}

func detectHostSource(getenv func(string) string) string {
	if source := strings.TrimSpace(getenv("PIPPIT_CLI_SOURCE")); source != "" {
		return source
	}
	source := ""
	for _, marker := range []struct{ key, host string }{
		{"CODEX_THREAD_ID", "codex"},
		{"CODEX_SESSION_ID", "codex"},
		{"CLAUDECODE", "claude_code"},
		{"CURSOR_AGENT", "cursor"},
		{"GEMINI_CLI", "gemini_cli"},
	} {
		value := strings.ToLower(strings.TrimSpace(getenv(marker.key)))
		if value == "" || value == "0" || value == "false" {
			continue
		}
		if source != "" && source != marker.host {
			return ""
		}
		source = marker.host
	}
	return source
}
