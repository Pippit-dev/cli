package common

import "testing"

func TestDetectHostSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"unknown", nil, ""},
		{"trae", map[string]string{"DREAMINA_CLI_CALLER_AGENT": " Trae "}, "trae"},
		{"trae conflict", map[string]string{"DREAMINA_CLI_CALLER_AGENT": "trae", "CURSOR_AGENT": "1"}, ""},
		{"doubao", map[string]string{"DOUBAO_OFFICE_AGENT_NAME": "private", "DOUBAO_OFFICE_APP_ID": "app"}, "doubao"},
		{"incomplete doubao", map[string]string{"DOUBAO_OFFICE_AGENT_NAME": "private"}, ""},
		{"workbuddy desktop", map[string]string{"CODEBUDDY_HOST": "workbuddy-desktop"}, "workbuddy"},
		{"other codebuddy", map[string]string{"CODEBUDDY_HOST": "other"}, ""},
		{"desktop conflict", map[string]string{"CODEBUDDY_HOST": "workbuddy-desktop", "CODEX_THREAD_ID": "id"}, ""},
		{"office alias", map[string]string{"PIPPIT_CLI_SOURCE": " doubao_office "}, "doubao"},
		{"explicit host", map[string]string{"PIPPIT_CLI_SOURCE": " workbuddy ", "CODEX_THREAD_ID": "secret"}, "workbuddy"},
		{"codex", map[string]string{"CODEX_THREAD_ID": "secret", "CODEX_SESSION_ID": "other-secret"}, "codex"},
		{"claude", map[string]string{"CLAUDECODE": "1"}, "claude_code"},
		{"cursor", map[string]string{"CURSOR_AGENT": "1"}, "cursor"},
		{"gemini", map[string]string{"GEMINI_CLI": "1"}, "gemini_cli"},
		{"opencode", map[string]string{"OPENCODE": "1"}, "opencode"},
		{"qwen", map[string]string{"QWEN_CODE": "1"}, "qwen_code"},
		{"cline", map[string]string{"CLINE_ACTIVE": " true "}, "cline"},
		{"copilot cli", map[string]string{"COPILOT_CLI": "1"}, "github_copilot_cli"},
		{"copilot session", map[string]string{"COPILOT_AGENT_SESSION_ID": "secret"}, "github_copilot_cli"},
		{"copilot markers agree", map[string]string{"COPILOT_CLI": "1", "COPILOT_AGENT_SESSION_ID": "secret"}, "github_copilot_cli"},
		{"ambiguous", map[string]string{"CODEX_THREAD_ID": "secret", "CURSOR_AGENT": "1"}, ""},
		{"new markers conflict", map[string]string{"OPENCODE": "1", "CLINE_ACTIVE": "true"}, ""},
		{"disabled markers", map[string]string{"CLAUDECODE": "0", "GEMINI_CLI": " false "}, ""},
		{"new markers disabled", map[string]string{"OPENCODE": "0", "QWEN_CODE": " FALSE ", "CLINE_ACTIVE": "", "COPILOT_CLI": "false", "COPILOT_AGENT_SESSION_ID": " "}, ""},
		{"configuration is not runtime evidence", map[string]string{"CODEX_HOME": "/home/example", "ANTHROPIC_API_KEY": "secret", "TERM_PROGRAM": "vscode", "AGENT": "1", "OPENCODE_CONFIG": "/example/config.json", "QWEN_API_KEY": "secret", "COPILOT_HOME": "/example/copilot"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectHostSource(func(key string) string { return tc.env[key] }, nil); got != tc.want {
				t.Errorf("source = %q, want %q", got, tc.want)
			}
		})
	}
}
