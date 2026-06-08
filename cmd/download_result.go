package cmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

func newDownloadResultCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts common.DownloadResultOptions

	cmd := &cobra.Command{
		Use:   "download-result",
		Short: "Download a generated result URL",
		Args:  cobra.NoArgs,
		RunE: withErrorLog("download-result", func() map[string]string {
			fields := map[string]string{
				"output_path": opts.OutputPath,
				"has_url":     strconv.FormatBool(strings.TrimSpace(opts.URL) != ""),
				"workers":     strconv.Itoa(opts.Workers),
			}
			if opts.UpdatedAt > 0 {
				fields["updated_at"] = strconv.FormatInt(opts.UpdatedAt, 10)
			}
			return fields
		}, func(cmd *cobra.Command, _ []string) error {
			opts.OutputPath = strings.TrimSpace(opts.OutputPath)
			if opts.OutputPath == "" {
				return fmt.Errorf("--output-path is required")
			}
			opts.URL = strings.TrimSpace(opts.URL)
			if opts.URL == "" {
				return fmt.Errorf("--url is required")
			}
			if opts.Workers <= 0 {
				return fmt.Errorf("--workers must be greater than 0")
			}

			result, err := common.DownloadResult(cmd.Context(), opts, runner)
			if err != nil {
				return err
			}
			return common.WriteJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.URL, "url", "", "URL to download")
	cmd.Flags().StringVar(&opts.OutputPath, "output-path", "", "local output file path")
	cmd.Flags().Int64Var(&opts.UpdatedAt, "updated-at", 0, "remote file update time as a Unix timestamp")
	cmd.Flags().IntVar(&opts.Workers, "workers", 5, "parallel download workers")
	return cmd
}
