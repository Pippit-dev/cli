package trae

import (
	"context"
	"testing"
)

func TestCallerChain(t *testing.T) {
	for _, tc := range []struct {
		chain []string
		want  bool
	}{
		{[]string{"/bin/sh", "/Applications/Trae CN.app/Contents/Frameworks/Electron"}, true},
		{[]string{"node", `C:\Users\agent\AppData\Local\Programs\Trae CN\Trae CN.exe`}, true},
		{[]string{"powershell.exe", `C:\Users\agent\AppData\Local\Programs\Trae\Trae.exe`}, true},
		{[]string{"powershell.exe", `C:\Tools\MyTrae.exe`}, false},
		{[]string{"/usr/bin/bash", "/opt/traex"}, true},
		{[]string{"/usr/bin/codex", "/Applications/Trae.app/Contents/MacOS/Electron"}, false},
		{[]string{"/Applications/Trae CN.app/Contents/bin/aha_doctor"}, false},
		{[]string{"/tmp/not-trae.app/bin/node", "/bin/sh"}, false},
		{[]string{"node", "sh", "sh", "sh", "sh", "sh", "sh", "sh", "sh", "sh", "traex"}, false},
	} {
		i := 0
		got := walk(context.Background(), 100, func(_ context.Context, pid int) (int, string) {
			if i == len(tc.chain) {
				return 0, ""
			}
			exe := tc.chain[i]
			i++
			return pid - 1, exe
		})
		if got != tc.want {
			t.Errorf("%v: got %t", tc.chain, got)
		}
	}
}
