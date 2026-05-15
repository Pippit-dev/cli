package novelcmd

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal"
	"github.com/bytedance/sonic"
	"github.com/spf13/cobra"

	"github.com/Pippit-dev/pippit-cli/internal/novel"
)

// NewCommand builds the novel scene command tree.
func NewCommand(stdout, stderr io.Writer, apiClient *internal.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "novel",
		Short: "Novel generation workflows",
	}

	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	_ = apiClient // Reserved for the real novel client once endpoints are wired.
	novelClient := novel.MockClient{}
	cmd.AddCommand(newNovelSubmitRunCommand(stdout, stderr, novelClient))
	cmd.AddCommand(newNovelUploadFileCommand(stdout, stderr, novelClient))
	cmd.AddCommand(newNovelGetThreadCommand(stdout, stderr, novelClient))
	return cmd
}

func newNovelSubmitRunCommand(stdout, stderr io.Writer, client novel.Client) *cobra.Command {
	var opts novel.SubmitRunOptions

	cmd := &cobra.Command{
		Use:   "+submit-run",
		Short: "Submit a Run task for the novel scene",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Prompt = strings.TrimSpace(opts.Prompt)
			opts.Title = strings.TrimSpace(opts.Title)
			opts.Author = strings.TrimSpace(opts.Author)
			opts.Agent = strings.TrimSpace(opts.Agent)

			if opts.Prompt == "" {
				return fmt.Errorf("--prompt is required")
			}
			if opts.Agent == "" {
				opts.Agent = novel.DefaultAgent
			}
			opts.Mock = true

			result, err := client.SubmitRun(cmd.Context(), opts)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.Prompt, "prompt", "", "novel generation prompt")
	cmd.Flags().StringVar(&opts.Title, "title", "", "novel title")
	cmd.Flags().StringVar(&opts.Author, "author", "", "novel author")
	cmd.Flags().StringVar(&opts.Agent, "agent", novel.DefaultAgent, "agent used to submit the run")
	return cmd
}

func newNovelUploadFileCommand(stdout, stderr io.Writer, client novel.Client) *cobra.Command {
	var opts novel.UploadFileOptions

	cmd := &cobra.Command{
		Use:   "+upload-file",
		Short: "Upload a file for the novel scene",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.Path = strings.TrimSpace(opts.Path)
			opts.Purpose = strings.TrimSpace(opts.Purpose)

			if opts.Path == "" {
				return fmt.Errorf("--path is required")
			}
			if opts.Purpose == "" {
				opts.Purpose = novel.DefaultFilePurpose
			}
			opts.FileName = filepath.Base(opts.Path)
			opts.Mock = true

			result, err := client.UploadFile(cmd.Context(), opts)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.Path, "path", "", "local file path to upload")
	cmd.Flags().StringVar(&opts.Purpose, "purpose", novel.DefaultFilePurpose, "file purpose")
	return cmd
}

func newNovelGetThreadCommand(stdout, stderr io.Writer, client novel.Client) *cobra.Command {
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
			opts.Mock = true

			result, err := client.GetThread(cmd.Context(), opts)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.ThreadID, "thread-id", "", "thread ID to fetch")
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
