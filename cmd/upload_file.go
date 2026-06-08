package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

func newUploadFileCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts common.UploadFileOptions

	cmd := &cobra.Command{
		Use:   "upload-file",
		Short: "Upload a file",
		Args:  cobra.NoArgs,
		RunE: withErrorLog("upload-file", func() map[string]string {
			return map[string]string{
				"file_name": fileNameForLog(opts.Path),
			}
		}, func(cmd *cobra.Command, _ []string) error {
			opts.Path = strings.TrimSpace(opts.Path)

			if opts.Path == "" {
				return fmt.Errorf("--path is required")
			}

			result, err := common.UploadFile(cmd.Context(), opts, runner)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.Path, "path", "", "local file path to upload")
	return cmd
}

func fileNameForLog(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}
