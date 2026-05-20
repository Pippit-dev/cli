package novelcmd

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/bytedance/sonic"
	"github.com/spf13/cobra"

	"github.com/Pippit-dev/pippit-cli/internal/novel"
)

// NewCommand builds the novel scene command tree.
func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "novel",
		Short: "Novel generation workflows",
	}

	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.AddCommand(newNovelSubmitRunCommand(stdout, stderr, runner))
	cmd.AddCommand(newNovelUploadFileCommand(stdout, stderr, runner))
	cmd.AddCommand(newNovelGetThreadCommand(stdout, stderr, runner))
	return cmd
}

func newNovelSubmitRunCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts novel.SubmitRunOptions

	cmd := &cobra.Command{
		Use:   "+submit-run",
		Short: "Submit a Run task for the novel scene",
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

			result, err := novel.SubmitRun(cmd.Context(), &opts, runner)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.Message, "message", "", "message to send to the novel agent")
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "existing thread ID; omit to create a new thread")
	cmd.Flags().StringArrayVar(&opts.AssetIDs, "asset-ids", nil, "asset ID to attach; repeat for multiple assets")
	cmd.Flags().StringVar(&opts.AgentName, "agent-name", "", "agent name to submit the run to")
	return cmd
}

func newNovelUploadFileCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts novel.UploadFileOptions

	cmd := &cobra.Command{
		Use:   "+upload-file",
		Short: "Upload a file for the novel scene",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Path = strings.TrimSpace(opts.Path)

			if opts.Path == "" {
				return fmt.Errorf("--path is required")
			}
			opts.FileName = filepath.Base(opts.Path)
			opts.Mock = true

			result, err := novel.UploadFile(cmd.Context(), opts, runner)
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

func newNovelGetThreadCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts novel.GetThreadOptions

	cmd := &cobra.Command{
		Use:   "+get-thread",
		Short: "Get a novel thread summary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.ThreadID = strings.TrimSpace(opts.ThreadID)
			if opts.ThreadID == "" {
				return fmt.Errorf("--thread-id is required")
			}
			opts.RunID = strings.TrimSpace(opts.RunID)

			result, err := novel.GetThread(cmd.Context(), &opts, runner)
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
