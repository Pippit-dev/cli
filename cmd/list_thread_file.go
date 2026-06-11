package cmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

func newListThreadFileCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts common.ListThreadFileOptions

	cmd := &cobra.Command{
		Use:   "list-thread-file",
		Short: "List files in a thread",
		Args:  cobra.NoArgs,
		RunE: withErrorLog("list-thread-file", func() map[string]string {
			return map[string]string{
				"thread_id": opts.ThreadID,
				"page_num":  strconv.Itoa(opts.PageNum),
				"page_size": strconv.Itoa(opts.PageSize),
			}
		}, func(cmd *cobra.Command, _ []string) error {
			opts.ThreadID = strings.TrimSpace(opts.ThreadID)
			if opts.ThreadID == "" {
				return fmt.Errorf("--thread-id is required")
			}
			if opts.PageSize <= 0 || opts.PageSize > common.MaxListThreadFilePageSize {
				return fmt.Errorf("--page-size must be between 1 and %d", common.MaxListThreadFilePageSize)
			}
			if opts.PageNum <= 0 {
				return fmt.Errorf("--page-num must be greater than 0")
			}

			result, err := common.ListThreadFile(cmd.Context(), &opts, runner)
			if err != nil {
				return err
			}
			return common.WriteJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "thread ID to list files for")
	cmd.Flags().IntVar(&opts.PageNum, "page-num", 1, "page number (1-based)")
	cmd.Flags().IntVar(&opts.PageSize, "page-size", common.MaxListThreadFilePageSize, fmt.Sprintf("number of files per page (between 1 and %d)", common.MaxListThreadFilePageSize))
	return cmd
}
