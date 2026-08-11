package canvas

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type delayedImportPromptReader struct {
	io.Reader
	once sync.Once
}

func (reader *delayedImportPromptReader) Read(buffer []byte) (int, error) {
	reader.once.Do(func() { time.Sleep(25 * time.Millisecond) })
	return reader.Reader.Read(buffer)
}

func TestImportPromptTUISelectUsesArrowKeys(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var output bytes.Buffer
	session := newImportPromptSessionWithTUI(
		ctx,
		&delayedImportPromptReader{Reader: bytes.NewBufferString("\x1b[B\r")},
		&output,
		true,
	)
	selected, err := session.askChoice(
		"断点续跑记录：",
		[]importPromptChoice{
			{label: "自动生成（推荐）"},
			{label: "自定义路径"},
		},
		1,
	)
	if err != nil {
		t.Fatalf("askChoice() error = %v; output = %q", err, output.String())
	}
	if selected != 2 {
		t.Fatalf("askChoice() = %d, want arrow-down selection 2", selected)
	}
	for _, expected := range []string{"断点续跑记录", "自动生成", "自定义路径", "使用 ↑/↓ 切换，按 Enter 确认"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("TUI output missing %q: %q", expected, output.String())
		}
	}
}

func TestImportPromptTUIReadsPastedURL(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var output bytes.Buffer
	session := newImportPromptSessionWithTUI(
		ctx,
		&delayedImportPromptReader{Reader: bytes.NewBufferString(testLibTVURL + "\r")},
		&output,
		true,
	)
	value, eof, err := session.readLine("LibTV 画布链接：")
	if err != nil {
		t.Fatalf("readLine() error = %v; output = %q", err, output.String())
	}
	if eof {
		t.Fatal("TUI readLine() unexpectedly reported EOF")
	}
	if value != testLibTVURL {
		t.Fatalf("readLine() = %q, want pasted URL", value)
	}
	for _, expected := range []string{"LibTV 画布链接", "粘贴内容后按 Enter 确认"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("TUI output missing %q: %q", expected, output.String())
		}
	}
}

func TestImportPromptTUIHonorsContextCancellation(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	session := newImportPromptSessionWithTUI(ctx, bytes.NewBuffer(nil), &output, true)
	_, err := session.askChoice(
		"导入完成后：",
		[]importPromptChoice{{label: "打开画布"}, {label: "暂不打开"}},
		1,
	)
	if err == nil || !errors.Is(err, errCanvasImportSetupCanceled) {
		t.Fatalf("askChoice() error = %v, want actionable cancellation", err)
	}
}
