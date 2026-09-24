package common

import "testing"

func TestDetectHostSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"unknown", nil, ""},
		{"explicit host", map[string]string{"PIPPIT_CLI_SOURCE": " workbuddy ", "CODEX_THREAD_ID": "secret"}, "workbuddy"},
		{"codex", map[string]string{"CODEX_THREAD_ID": "secret", "CODEX_SESSION_ID": "other-secret"}, "codex"},
		{"claude", map[string]string{"CLAUDECODE": "1"}, "claude_code"},
		{"cursor", map[string]string{"CURSOR_AGENT": "1"}, "cursor"},
		{"gemini", map[string]string{"GEMINI_CLI": "1"}, "gemini_cli"},
		{"ambiguous", map[string]string{"CODEX_THREAD_ID": "secret", "CURSOR_AGENT": "1"}, ""},
		{"disabled markers", map[string]string{"CLAUDECODE": "0", "GEMINI_CLI": " false "}, ""},
		{"configuration is not runtime evidence", map[string]string{"CODEX_HOME": "/home/example", "ANTHROPIC_API_KEY": "secret", "TERM_PROGRAM": "vscode"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectHostSource(func(key string) string { return tc.env[key] }); got != tc.want {
				t.Errorf("source = %q, want %q", got, tc.want)
			}
		})
	}
}
