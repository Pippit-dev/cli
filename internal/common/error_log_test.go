package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/sonic"
)

func TestAppendDailyErrorLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	err := AppendDailyErrorLog("short-drama +get-thread", fmt.Errorf("request failed"), map[string]string{
		"thread_id":  "thread_123",
		"access_key": "secret",
		"empty":      "",
	})
	if err != nil {
		t.Fatalf("AppendDailyErrorLog(): %v", err)
	}
	if err := AppendDailyErrorLog("short-drama +get-thread", fmt.Errorf("second failure"), nil); err != nil {
		t.Fatalf("AppendDailyErrorLog() second call: %v", err)
	}

	path := filepath.Join(home, ".pippit_tool_cli", "logs", time.Now().Format("2006-01-02")+".log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines = %d, want 2\n%s", len(lines), string(data))
	}

	var first map[string]any
	if err := sonic.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode first log line: %v\n%s", err, lines[0])
	}
	if first["command"] != "short-drama +get-thread" {
		t.Fatalf("command = %v, want get-thread command", first["command"])
	}
	if first["error"] != "request failed" {
		t.Fatalf("error = %v, want request failed", first["error"])
	}
	fields, ok := first["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields = %#v, want object", first["fields"])
	}
	if fields["thread_id"] != "thread_123" {
		t.Fatalf("thread_id = %v, want thread_123", fields["thread_id"])
	}
	if _, ok := fields["access_key"]; ok {
		t.Fatalf("access_key should be omitted from log fields: %#v", fields)
	}
}
