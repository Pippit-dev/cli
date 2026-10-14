package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestSubmitRunIsNotExposedInRootHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newRootCommand(&stdout, &stderr, nil)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "submit-run") || strings.Contains(stdout.String(), "creative conversations") {
		t.Fatalf("root help still exposes creative conversations: %s", stdout.String())
	}
	command, _, err := root.Find([]string{"short-drama", "+submit-run"})
	if err != nil || command == nil || command.Name() != "+submit-run" {
		t.Fatalf("short-drama submission must remain available: command=%v, err=%v", command, err)
	}
}

func TestSubmitRunIsUnavailable(t *testing.T) {
	for _, args := range [][]string{
		{"submit-run"},
		{"submit-run", "--message", "生成一个小猫在花园散步的视频"},
		{"submit-run", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		root := newRootCommand(&stdout, &stderr, nil)
		root.SetArgs(args)
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("Execute(%v) = %v, want unknown command", args, err)
		}
		if stdout.Len() != 0 {
			t.Fatalf("disabled command produced output: %s", stdout.String())
		}
	}
}
