package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands a leading "~" to the current user's home directory.
func ExpandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("解析用户主目录失败: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
