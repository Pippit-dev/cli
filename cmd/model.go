package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/models"
	"github.com/spf13/cobra"
)

func newModelCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var refresh bool
	var modelType string
	service := models.NewService(runner)
	query := func(cmd *cobra.Command, args []string, describe bool) error {
		if modelType != "video" {
			return fmt.Errorf("当前仅支持 --type video")
		}
		result, err := service.Get(cmd.Context(), refresh)
		if err != nil {
			return err
		}
		if result.Warning != "" {
			fmt.Fprintln(stderr, result.Warning)
		}
		output := map[string]any{
			"scene":  result.Catalog.Scene,
			"cached": result.Cached, "fetched_at": result.FetchedAt,
			"expires_at": result.FetchedAt.Add(models.CacheTTL).Format(time.RFC3339Nano),
		}
		if describe {
			model, err := result.Catalog.Describe(strings.Join(args, " "))
			if err != nil {
				return err
			}
			output["model"] = model
		} else {
			output["models"] = result.Catalog.Search(strings.Join(args, " "))
		}
		return common.WriteJSON(stdout, output)
	}
	cmd := &cobra.Command{
		Use:   "model [key]",
		Short: "Discover available video models and their server configuration",
		Long:  "Query available video models using your current credentials. Successful queries are cached locally for 5 minutes. Use --refresh to bypass the cache; retry if a query fails.",
		Args:  cobra.MaximumNArgs(1),
		RunE: withErrorLog("model", nil, func(cmd *cobra.Command, args []string) error {
			return query(cmd, args, len(args) > 0)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.PersistentFlags().BoolVar(&refresh, "refresh", false, "refresh the 5-minute local model cache")
	cmd.PersistentFlags().StringVarP(&modelType, "type", "t", "video", "model type (currently video only)")
	cmd.AddCommand(&cobra.Command{
		Use: "list [query]", Aliases: []string{"search"},
		Short: "List video models, optionally filtered by key or name",
		Args:  cobra.MaximumNArgs(1),
		RunE: withErrorLog("model list", nil, func(cmd *cobra.Command, args []string) error {
			return query(cmd, args, false)
		}),
	})
	cmd.AddCommand(&cobra.Command{
		Use: "describe <key>", Short: "Show model parameters with CLI-ready ratios, defaults, and limits",
		Args: cobra.ExactArgs(1),
		RunE: withErrorLog("model describe", nil, func(cmd *cobra.Command, args []string) error {
			return query(cmd, args, true)
		}),
	})
	return cmd
}
