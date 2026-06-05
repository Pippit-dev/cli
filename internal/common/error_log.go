package common

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bytedance/sonic"
)

const (
	errorLogDirName = ".pippit_tool_cli"
	errorLogSubdir  = "logs"
)

type errorLogEntry struct {
	Time    string            `json:"time"`
	Command string            `json:"command"`
	Fields  map[string]string `json:"fields,omitempty"`
	Error   string            `json:"error"`
}

// AppendDailyErrorLog appends one CLI error record to ~/.pippit_tool_cli/logs/yyyy-mm-dd.log.
func AppendDailyErrorLog(command string, err error, fields map[string]string) error {
	if err == nil {
		return nil
	}
	now := time.Now()
	path, pathErr := dailyErrorLogPath(now)
	if pathErr != nil {
		return pathErr
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
		return mkErr
	}

	entry := errorLogEntry{
		Time:    now.Format(time.RFC3339),
		Command: strings.TrimSpace(command),
		Fields:  cleanErrorLogFields(fields),
		Error:   err.Error(),
	}
	data, marshalErr := sonic.Marshal(entry)
	if marshalErr != nil {
		return marshalErr
	}
	data = append(data, '\n')
	file, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if openErr != nil {
		return openErr
	}
	defer file.Close()
	_, writeErr := file.Write(data)
	return writeErr
}

func dailyErrorLogPath(now time.Time) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, errorLogDirName, errorLogSubdir, now.Format("2006-01-02")+".log"), nil
}

func cleanErrorLogFields(fields map[string]string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	cleaned := make(map[string]string, len(fields))
	for k, v := range fields {
		key := strings.TrimSpace(k)
		value := strings.TrimSpace(v)
		if key == "" || value == "" || isSensitiveLogField(key) {
			continue
		}
		cleaned[key] = value
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func isSensitiveLogField(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "access") ||
		strings.Contains(key, "authorization") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "token")
}
