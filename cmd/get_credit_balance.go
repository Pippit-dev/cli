package cmd

import (
	"io"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

func newGetCreditBalanceCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var withLogID bool
	cmd := &cobra.Command{
		Use:   "get-credit-balance",
		Short: "Get the effective credit balance",
		Args:  cobra.NoArgs,
		RunE: withErrorLog("get-credit-balance", nil, func(cmd *cobra.Command, _ []string) error {
			result, err := common.GetCreditBalance(cmd.Context(), runner)
			if err != nil {
				return err
			}
			if withLogID {
				return common.WriteJSON(stdout, struct {
					TotalRemainAmount int64  `json:"total_remain_amount,string"`
					LogID             string `json:"log_id"`
				}{
					TotalRemainAmount: result.TotalRemainAmount,
					LogID:             result.LogID,
				})
			}
			return common.WriteJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().BoolVar(&withLogID, "with-log-id", false, "include the request log ID in JSON output")
	return cmd
}
