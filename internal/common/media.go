package common

import (
	"fmt"
	"path/filepath"
	"strings"
)

var (
	allowedVideoExtensionList = []string{".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mkv", ".m4v"}
	allowedVideoExtensions    = StringSet(allowedVideoExtensionList)
)

// ValidateVideoExtensions validates local video paths against the CLI-supported suffixes.
func ValidateVideoExtensions(paths []string) error {
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
		if _, ok := allowedVideoExtensions[ext]; !ok {
			return fmt.Errorf("不支持的视频文件后缀 %q，文件：%q；支持的后缀：%s", ext, path, strings.Join(allowedVideoExtensionList, ", "))
		}
	}
	return nil
}
