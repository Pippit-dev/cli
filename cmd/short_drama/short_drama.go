package short_drama

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/short_drama"
	"github.com/bytedance/sonic"
	"github.com/spf13/cobra"
)

// NewCommand builds the short_drama scene command tree.
func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "short-drama",
		Short: "Short drama generation workflows",
	}

	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.AddCommand(newShortDramaSubmitRunCommand(stdout, stderr, runner))
	cmd.AddCommand(newShortDramaUploadFileCommand(stdout, stderr, runner))
	cmd.AddCommand(newShortDramaDownloadResultCommand(stdout, stderr, runner))
	cmd.AddCommand(newShortDramaGetThreadCommand(stdout, stderr, runner))
	cmd.AddCommand(newShortDramaListThreadFileCommand(stdout, stderr, runner))
	return cmd
}

func newShortDramaSubmitRunCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts short_drama.SubmitRunOptions

	cmd := &cobra.Command{
		Use:   "+submit-run",
		Short: "Submit a Run task for the short drama scene",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Message = strings.TrimSpace(opts.Message)
			opts.ThreadID = strings.TrimSpace(opts.ThreadID)

			if opts.Message == "" {
				return fmt.Errorf("--message is required")
			}

			result, err := short_drama.SubmitRun(cmd.Context(), &opts, runner)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.Message, "message", "", "message to send to the short drama agent")
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "existing thread ID; omit to create a new thread")
	cmd.Flags().StringArrayVar(&opts.AssetIDs, "asset-ids", nil, "asset ID to attach; repeat for multiple assets")
	return cmd
}

func newShortDramaUploadFileCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts common.UploadFileOptions

	cmd := &cobra.Command{
		Use:   "+upload-file",
		Short: "Upload a file for the short drama scene",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Path = strings.TrimSpace(opts.Path)

			if opts.Path == "" {
				return fmt.Errorf("--path is required")
			}
			opts.FileName = filepath.Base(opts.Path)
			opts.Mock = true

			result, err := common.UploadFile(cmd.Context(), opts, runner)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.Path, "path", "", "local file path to upload")
	return cmd
}

func newShortDramaDownloadResultCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts common.DownloadResultOptions

	cmd := &cobra.Command{
		Use:   "+download-result",
		Short: "Download a generated result URL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.URL, "url", "", "URL to download")
	cmd.Flags().StringVar(&opts.OutputPath, "output-path", "", "local output file path")
	cmd.Flags().IntVar(&opts.Workers, "workers", 5, "parallel download workers")
	return cmd
}

func newShortDramaGetThreadCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts short_drama.GetThreadOptions

	cmd := &cobra.Command{
		Use:   "+get-thread",
		Short: "Get a short drama thread detail",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.ThreadID = strings.TrimSpace(opts.ThreadID)
			if opts.ThreadID == "" {
				return fmt.Errorf("--thread-id is required")
			}
			opts.RunID = strings.TrimSpace(opts.RunID)

			result, err := short_drama.GetThread(cmd.Context(), &opts, runner)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "thread ID to fetch")
	cmd.Flags().StringVar(&opts.RunID, "run-id", "", "run ID to fetch")
	cmd.Flags().IntVar(&opts.AfterSeq, "after-seq", 0, "return messages whose sequence is greater than or equal to this value")
	return cmd
}

func newShortDramaListThreadFileCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts common.ListThreadFileOptions

	cmd := &cobra.Command{
		Use:   "+list-thread-file",
		Short: "List files in a short drama thread",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.ThreadID = strings.TrimSpace(opts.ThreadID)
			if opts.ThreadID == "" {
				return fmt.Errorf("--thread-id is required")
			}
			if opts.PageSize <= 0 || opts.PageSize > 1000 {
				return fmt.Errorf("--page-size must be between 1 and 1000")
			}
			if opts.PageNum <= 0 {
				return fmt.Errorf("--page-num must be greater than 0")
			}

			result, err := common.ListThreadFile(cmd.Context(), &opts, runner)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "thread ID to list files for")
	cmd.Flags().IntVar(&opts.PageNum, "page-num", 1, "page number (1-based)")
	cmd.Flags().IntVar(&opts.PageSize, "page-size", 1000, "number of files per page (between 1 and 1000)")
	return cmd
}

func writeJSON(w io.Writer, v any) error {
	data, err := sonic.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
