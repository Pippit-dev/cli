package common

import (
	"strings"
	"testing"
)

func TestValidateVideoExtensions(t *testing.T) {
	if err := ValidateVideoExtensions([]string{"source.MP4", "reference.webm"}); err != nil {
		t.Fatalf("ValidateVideoExtensions() error = %v, want nil", err)
	}

	err := ValidateVideoExtensions([]string{"source.txt"})
	if err == nil || !strings.Contains(err.Error(), `不支持的视频文件后缀 ".txt"`) {
		t.Fatalf("ValidateVideoExtensions() error = %v, want extension validation", err)
	}
}
