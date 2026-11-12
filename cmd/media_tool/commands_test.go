package media_tool

import (
	"bytes"
	"strings"
	"testing"
)

func TestUnsupportedFlagsNeverReachSubmission(t *testing.T) {
	for _, video := range []bool{false, true} {
		var out, errout bytes.Buffer
		cmd := NewImageSuperResolutionCommand(&out, &errout, nil)
		flags := []string{"--asset-id", "asset", "--background-color", "green"}
		if video {
			cmd = NewVideoMattingCommand(&out, &errout, nil)
			flags = []string{"--asset-id", "asset", "--quality", "HQ"}
		}
		cmd.SetArgs(flags)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("invalid params: %v", err)
		}
		if strings.Contains(out.String(), "submitting") || strings.Contains(errout.String(), "thread_id") {
			t.Fatal("invalid request began submission")
		}
	}
}
