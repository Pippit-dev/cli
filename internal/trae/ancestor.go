// Package trae identifies an invoking Trae process from its executable identity.
package trae

import (
	"context"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// Ancestor is stable for the duration of a CLI invocation.
var Ancestor = sync.OnceValue(func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	return walk(ctx, os.Getppid(), parentExecutable)
})

func walk(ctx context.Context, pid int, lookup func(context.Context, int) (int, string)) bool {
	for depth := 0; depth < 10 && pid > 1 && ctx.Err() == nil; depth++ {
		parent, executable := lookup(ctx, pid)
		if ctx.Err() != nil {
			return false
		}
		p := strings.ToLower(strings.ReplaceAll(executable, "\\", "/"))
		name := strings.TrimSuffix(path.Base(p), ".exe")
		switch name {
		case "traex", "trae cn":
			return true
		case "codex", "claude", "cursor", "workbuddy", "doubaowork", "agentinfraservice", "agentinfraservic":
			return false
		case "aha_doctor":
			// This diagnostic sidecar alone is not an IDE session.
		default:
			if strings.Contains(p, "/trae.app/") || strings.Contains(p, "/trae cn.app/") ||
				strings.Contains(p, "/programs/trae cn/") || strings.HasSuffix(p, "/.local/bin/coco") ||
				(strings.Contains(p, "/agent-tool-host/") || name == "agent-tool-host") || (strings.Contains(name, "trae") && strings.Contains(name, "helper")) {
				return true
			}
			for _, other := range []string{"/cursor.app/", "/workbuddy.app/", "/chatgpt.app/", "/doubaowork.app/", "/.codex/", "/@openai/codex/", "/@anthropic-ai/claude-code/"} {
				if strings.Contains(p, other) {
					return false
				}
			}
		}
		if parent == pid {
			break
		}
		pid = parent
	}
	return false
}
