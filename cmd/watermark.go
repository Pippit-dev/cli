package cmd

import (
	"fmt"
	"io"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/commandnames"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

func newWatermarkCommand(stdout, stderr io.Writer, runner *common.Runner, policy adapter.HTTPAdapter) *cobra.Command {
	cmd := &cobra.Command{
		Use:       commandnames.Watermark + " [on|off]",
		Short:     "Query or set the current account's AIGC watermark",
		Long:      "Without arguments, query the current account's AIGC watermark setting. Use on to retain it or off to remove it from saved content. This changes a persistent account setting; team selection does not change its scope. Final downloads remain subject to server policies.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: withErrorLog(commandnames.Watermark, nil, func(cmd *cobra.Command, args []string) error {
			var enabled *bool
			if len(args) > 0 {
				if args[0] != "on" && args[0] != "off" {
					return fmt.Errorf("水印状态只支持 on 或 off")
				}
				value := args[0] == "on"
				enabled = &value
			}
			watermarkRunner := runner
			if enabled != nil {
				// Reject redirects for this write without changing the shared runner's client.
				writeRunner := *runner
				writeRunner.Client = common.NewNonRedirectingHTTPClientWithAdapter(runner.Config.BaseURL, runner.Config.HTTPTimeout, newRunnerAuthorizer(runner), adapter.WithPPE(policy, runner.Config.PPEEnv))
				watermarkRunner = &writeRunner
			}
			result, err := common.Watermark(cmd.Context(), watermarkRunner, enabled)
			if err != nil {
				return err
			}
			return common.WriteJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return cmd
}
