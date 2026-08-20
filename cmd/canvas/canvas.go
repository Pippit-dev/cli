package canvas

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	canvascore "github.com/Pippit-dev/pippit-cli/internal/canvas"
	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

const maxApplyRequestBytes = 64 << 20

// NewCommand builds the provider-neutral personal Canvas command tree.
func NewCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "canvas",
		Short: "Create and operate personal novel canvases",
		Args:  cobra.NoArgs,
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.AddCommand(newCreateCommand(stdout, stderr, runner))
	cmd.AddCommand(newGetCommand(stdout, stderr, runner))
	cmd.AddCommand(newAllocateCommand(stdout, stderr, runner))
	cmd.AddCommand(newApplyCommand(stdout, stderr, runner))
	cmd.AddCommand(newUploadCommand(stdout, stderr, runner))
	return cmd
}

func newAllocateCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var count int
	cmd := &cobra.Command{
		Use:   "allocate",
		Short: "Allocate IDs for assets created by a later Canvas transaction",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := canvascore.Allocate(cmd.Context(), count, runner)
			if err != nil {
				logCanvasError("canvas allocate", err, map[string]string{"count": fmt.Sprint(count)})
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().IntVar(&count, "count", 0, "number of unique Pippit asset IDs to allocate")
	return cmd
}

func newCreateCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts canvascore.CreateOptions
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a personal novel Canvas project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(opts.RequestID) == "" {
				requestID, err := newRequestID()
				if err != nil {
					return fmt.Errorf("generate canvas request ID: %w", err)
				}
				opts.RequestID = requestID
			}
			result, err := canvascore.Create(cmd.Context(), opts, runner)
			if result != nil {
				if writeErr := common.WriteJSON(stdout, result); writeErr != nil {
					return writeErr
				}
			}
			if err != nil {
				logCanvasError("canvas create", err, map[string]string{"request_id": opts.RequestID})
				return err
			}
			return nil
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&opts.Title, "title", "", "project title (maximum 50 characters)")
	flags.StringVar(&opts.RequestID, "request-id", "", "caller request ID; generated when omitted")
	flags.BoolVar(&opts.Wait, "wait", false, "wait for the novel overview artifact")
	flags.DurationVar(&opts.PollInterval, "poll-interval", time.Second, "artifact polling interval")
	flags.DurationVar(&opts.WaitTimeout, "timeout", 2*time.Minute, "maximum artifact wait time")
	return cmd
}

func newGetCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var assetIDs []string
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get personal Canvas assets by ID",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := canvascore.Get(cmd.Context(), canvascore.GetOptions{AssetIDs: assetIDs}, runner)
			if err != nil {
				logCanvasError("canvas get", err, map[string]string{"asset_count": fmt.Sprint(len(assetIDs))})
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringArrayVar(&assetIDs, "asset-id", nil, "Pippit asset ID; repeat for multiple assets")
	return cmd
}

func newApplyCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var filePath string
	var projectID string
	var transportResult bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply one Canvas patch transaction",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			request, err := readApplyRequest(cmd.InOrStdin(), filePath)
			if err != nil {
				return err
			}
			result, err := canvascore.Apply(cmd.Context(), canvascore.ApplyOptions{
				ProjectID:                   projectID,
				Request:                     request,
				AllowNonAcknowledgedResults: transportResult,
			}, runner)
			if err != nil {
				logCanvasError("canvas apply", err, map[string]string{
					"batch_id":     request.BatchID,
					"project_id":   projectID,
					"transactions": fmt.Sprint(len(request.Transactions)),
				})
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&filePath, "file", "-", "BatchPatch JSON request file, or - for stdin")
	flags.StringVar(&projectID, "project-id", "", "personal novel project ID as a decimal string")
	flags.BoolVar(&transportResult, "transport-result", false, "return explicit transaction outcomes for transport decoding")
	if err := flags.MarkHidden("transport-result"); err != nil {
		panic(err)
	}
	return cmd
}

func newUploadCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts canvascore.UploadOptions
	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload a file to personal assets and wait until it is queryable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := canvascore.Upload(cmd.Context(), opts, runner)
			if err != nil {
				logCanvasError("canvas upload", err, map[string]string{"file_name": filepath.Base(strings.TrimSpace(opts.Path))})
				return err
			}
			return common.WriteJSON(stdout, result)
		},
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	flags := cmd.Flags()
	flags.StringVar(&opts.Path, "path", "", "local file path to upload")
	flags.DurationVar(&opts.PollInterval, "poll-interval", time.Second, "asset visibility polling interval")
	flags.DurationVar(&opts.WaitTimeout, "timeout", 2*time.Minute, "maximum asset visibility wait time")
	return cmd
}

func newRequestID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "pippit_cli_canvas_" + hex.EncodeToString(random), nil
}

func readApplyRequest(stdin io.Reader, filePath string) (canvascore.ApplyRequest, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return canvascore.ApplyRequest{}, fmt.Errorf("canvas apply --file must not be empty")
	}
	var reader io.Reader
	var file *os.File
	if filePath == "-" {
		reader = stdin
	} else {
		var err error
		file, err = os.Open(filePath)
		if err != nil {
			return canvascore.ApplyRequest{}, fmt.Errorf("open canvas apply request: %w", err)
		}
		defer file.Close()
		reader = file
	}
	limited := io.LimitReader(reader, maxApplyRequestBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return canvascore.ApplyRequest{}, fmt.Errorf("read canvas apply request: %w", err)
	}
	if len(payload) > maxApplyRequestBytes {
		return canvascore.ApplyRequest{}, fmt.Errorf("canvas apply request exceeds %d bytes", maxApplyRequestBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var request canvascore.ApplyRequest
	if err := decoder.Decode(&request); err != nil {
		return canvascore.ApplyRequest{}, fmt.Errorf("decode canvas apply request: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return canvascore.ApplyRequest{}, err
	}
	return request, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing canvas apply data: %w", err)
	}
	return fmt.Errorf("canvas apply request must contain exactly one JSON object")
}

func logCanvasError(command string, err error, fields map[string]string) {
	_ = common.AppendDailyErrorLog(command, err, fields)
}
