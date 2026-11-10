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
		return NormalizeHostSource(source)
	}
	active := func(key string) bool {
		value := strings.ToLower(strings.TrimSpace(getenv(key)))
		return value != "" && value != "0" && value != "false"
	}
	hosts := make(map[string]bool)
	for _, marker := range []struct{ key, host string }{
		{"CODEX_THREAD_ID", "codex"},
		{"CODEX_SESSION_ID", "codex"},
		{"CLAUDECODE", "claude_code"},
		{"CURSOR_AGENT", "cursor"},
		{"GEMINI_CLI", "gemini_cli"},
		{"OPENCODE", "opencode"},
		{"QWEN_CODE", "qwen_code"},
		{"CLINE_ACTIVE", "cline"},
		{"COPILOT_CLI", "github_copilot_cli"},
		{"COPILOT_AGENT_SESSION_ID", "github_copilot_cli"},
	} {
		if active(marker.key) {
			hosts[marker.host] = true
		}
	}
	if active("DOUBAO_OFFICE_AGENT_NAME") && active("DOUBAO_OFFICE_APP_ID") {
		hosts["doubao"] = true
	}
	if strings.EqualFold(strings.TrimSpace(getenv("DREAMINA_CLI_CALLER_AGENT")), "trae") {
		hosts["trae"] = true
	}
	if getenv("CODEBUDDY_HOST") == "workbuddy-desktop" {
		hosts["workbuddy"] = true
	}
	if len(hosts) == 1 {
		for host := range hosts {
			return host
		}
	}
	return ""
}

// NormalizeHostSource maps the historical office alias to the analytics value.
func NormalizeHostSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "doubao_office" {
		return "doubao"
	}
	return source
}
