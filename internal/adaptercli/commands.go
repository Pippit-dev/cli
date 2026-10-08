// Package adaptercli configures Cobra command availability and help text from CLIOptions.
package adaptercli

import (
	"slices"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/commandnames"
	"github.com/spf13/cobra"
)

// Configure removes disallowed top-level commands and their aliases after registration.
// It retains allowed commands and all their subcommands, then applies the configured help text.
func Configure(root *cobra.Command, options adapter.CLIOptions) {
	commands := append([]*cobra.Command(nil), root.Commands()...)
	for _, child := range commands {
		name := child.Name()
		allowed := options.DefaultAllowRootCommands || slices.Contains(options.AllowedRootCommands, name)
		if !allowed || (!options.LocalCredentialsEnabled && slices.Contains([]string{commandnames.Login, commandnames.Logout, commandnames.Status}, name)) ||
			(!options.SelfUpdateEnabled && name == commandnames.Update) {
			root.RemoveCommand(child)
		}
	}
	if options.RootHelpOverride != "" {
		root.Long = options.RootHelpOverride
	}
}
