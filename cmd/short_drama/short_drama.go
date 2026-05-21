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
	cmd.AddCommand(newShortDramaDownloadResultsCommand(stdout, stderr, runner))
	cmd.AddCommand(newShortDramaGetThreadCommand(stdout, stderr, runner))
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
			opts.AgentName = strings.TrimSpace(opts.AgentName)

			if opts.Message == "" {
				return fmt.Errorf("--message is required")
			}
			if opts.AgentName == "" {
				return fmt.Errorf("--agent-name is required")
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
	cmd.Flags().StringVar(&opts.AgentName, "agent-name", "", "agent name to submit the run to")
	return cmd
}

func newShortDramaUploadFileCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts short_drama.UploadFileOptions

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

			result, err := short_drama.UploadFile(cmd.Context(), opts, runner)
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

func newShortDramaDownloadResultsCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts short_drama.DownloadResultsOptions

	cmd := &cobra.Command{
		Use:   "+download-results [urls...]",
		Short: "Download generated result URLs",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.OutputDir = strings.TrimSpace(opts.OutputDir)

			urls := make([]string, 0, len(opts.URLs)+len(args))
			for _, rawURL := range append(opts.URLs, args...) {
				rawURL = strings.TrimSpace(rawURL)
				if rawURL != "" {
					urls = append(urls, rawURL)
				}
			}
			opts.URLs = urls
			if len(opts.URLs) == 0 {
				return fmt.Errorf("--urls is required")
			}
			if opts.Workers <= 0 {
				return fmt.Errorf("--workers must be greater than 0")
			}

			result, err := short_drama.DownloadResults(cmd.Context(), opts, runner)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringArrayVar(&opts.URLs, "urls", nil, "URL to download; repeat or pass additional URLs after the flag")
	cmd.Flags().StringVar(&opts.OutputDir, "output-dir", "", "output directory; defaults to ./xyq_short_drama_output")
	cmd.Flags().IntVar(&opts.Workers, "workers", 5, "parallel download workers")
	return cmd
}

func newShortDramaGetThreadCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts short_drama.GetThreadOptions

	cmd := &cobra.Command{
		Use:   "+get-thread",
		Short: "Get a short drama thread summary",
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

func writeJSON(w io.Writer, v any) error {
	data, err := sonic.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
