package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

func newSubmitRunCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var body struct {
		Message  string   `json:"message"`
		ThreadID string   `json:"thread_id,omitempty"`
		AssetIDs []string `json:"asset_ids,omitempty"`
	}
	cmd := &cobra.Command{
		Use:   "submit-run",
		Short: "Create a creative conversation or send a message to an existing thread",
		Args:  cobra.NoArgs,
		RunE: withErrorLog("submit-run", nil, func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(body.Message) == "" {
				return fmt.Errorf("缺少必填参数 --message")
			}
			// Preserve the original message and identifiers; the backend owns routing.
			result, err := common.SubmitRun(cmd.Context(), "submit-run", body, runner)
			if err != nil {
				return err
			}
			return common.WriteJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&body.Message, "message", "", "original message to send (required)")
	cmd.Flags().StringVar(&body.ThreadID, "thread-id", "", "existing thread ID; omit to create a new thread")
	cmd.Flags().StringArrayVar(&body.AssetIDs, "asset-ids", nil, "asset ID to attach; repeat for multiple assets")
	return cmd
}
