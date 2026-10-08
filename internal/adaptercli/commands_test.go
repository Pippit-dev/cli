package adaptercli

import (
	"slices"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/spf13/cobra"
)

func TestConfigureDefaultAllowRootCommands(t *testing.T) {
	for _, tc := range []struct {
		name         string
		defaultAllow bool
		allowlist    []string
		want         []string
	}{
		{name: "默认拒绝且 nil 白名单"},
		{name: "默认拒绝且空白名单", allowlist: []string{}},
		{name: "默认拒绝且有白名单", allowlist: []string{"canvas"}, want: []string{"canvas"}},
		{name: "默认放行且 nil 白名单", defaultAllow: true, want: []string{"canvas", "future-command"}},
		{name: "默认放行且空白名单", defaultAllow: true, allowlist: []string{}, want: []string{"canvas", "future-command"}},
		{name: "默认放行且有白名单", defaultAllow: true, allowlist: []string{"canvas"}, want: []string{"canvas", "future-command"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := &cobra.Command{Use: "tool"}
			for _, name := range []string{"canvas", "future-command", "login", "logout", "status", "update"} {
				root.AddCommand(&cobra.Command{Use: name})
			}
			Configure(root, adapter.CLIOptions{DefaultAllowRootCommands: tc.defaultAllow, AllowedRootCommands: tc.allowlist})
			var got []string
			for _, child := range root.Commands() {
				got = append(got, child.Name())
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("命令范围异常: got=%v want=%v", got, tc.want)
			}
		})
	}
}
