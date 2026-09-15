package canvas

import (
	"fmt"

	"github.com/spf13/cobra"
)

// CommandDiscoveryHelp connects native transport help to the npm command catalogue.
const CommandDiscoveryHelp = `Canvas command catalogue (provided by the npm launcher):
  pippit-tool-cli canvas command list [--category <category>]
  pippit-tool-cli canvas command describe <name> [--operation <operation>] [--node-kind <kind>] [--path <schema.path>]
  pippit-tool-cli canvas command schema [name]
  pippit-tool-cli canvas command guide [topic]
  pippit-tool-cli canvas command run <name> --canvas-id <id> [--input <JSON> | --file <path|->]

Start with list, inspect inputs with describe/schema, then read guide for workflows.
Use the pippit-tool-cli installed by npm, or node <package>/scripts/run.js.
The standalone native binary provides Canvas transport commands and this help;
executing the command catalogue requires the npm launcher.`

func newCommandHelp() *cobra.Command {
	return &cobra.Command{
		Use:                "command",
		Short:              "Discover list, describe, schema, guide, and run via the npm launcher",
		Long:               CommandDiscoveryHelp,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
				return cmd.Help()
			}
			switch args[0] {
			case "list", "describe", "schema", "guide", "run":
				return fmt.Errorf("canvas command %s requires the npm launcher; use the npm-installed pippit-tool-cli or node <package>/scripts/run.js canvas command %s; the standalone native binary cannot execute this command", args[0], args[0])
			default:
				return fmt.Errorf("unknown canvas command action %q; expected list, describe, schema, guide, or run via the npm launcher; see canvas command --help", args[0])
			}
		},
	}
}
