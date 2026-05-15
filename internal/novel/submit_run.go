package novel

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"time"
)

const (
	DefaultAgent       = "pippit_novel_agent"
	DefaultFilePurpose = "novel_reference"
)

// SubmitRunOptions is the stable command-facing request shape for novel run submission.
type SubmitRunOptions struct {
	Prompt string `json:"prompt"`
	Title  string `json:"title,omitempty"`
	Author string `json:"author,omitempty"`
	Agent  string `json:"agent"`
	Mock   bool   `json:"mock"`
}

// SubmitRunResult is the JSON envelope printed by `pippit-cli novel +submit-run`.
type SubmitRunResult struct {
	Scene     string           `json:"scene"`
	ThreadID  string           `json:"thread_id"`
	RunID     string           `json:"run_id"`
	Status    string           `json:"status"`
	Submitted string           `json:"submitted_at"`
	Request   SubmitRunOptions `json:"request"`
}

// UploadFileOptions is the stable command-facing request shape for file upload.
type UploadFileOptions struct {
	Path     string `json:"path"`
	FileName string `json:"file_name"`
	Purpose  string `json:"purpose"`
	Mock     bool   `json:"mock"`
}

// UploadFileResult is the JSON envelope printed by `pippit-cli novel +upload-file`.
type UploadFileResult struct {
	Scene    string            `json:"scene"`
	FileID   string            `json:"file_id"`
	Status   string            `json:"status"`
	Uploaded string            `json:"uploaded_at"`
	Request  UploadFileOptions `json:"request"`
}

// GetThreadOptions is the stable command-facing request shape for thread lookup.
type GetThreadOptions struct {
	ThreadID string `json:"thread_id"`
	Mock     bool   `json:"mock"`
}

// ThreadRun is a compact run summary inside a thread.
type ThreadRun struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
	Scene  string `json:"scene"`
}

// GetThreadResult is the JSON envelope printed by `pippit-cli novel +get-thread`.
type GetThreadResult struct {
	Scene    string           `json:"scene"`
	ThreadID string           `json:"thread_id"`
	Status   string           `json:"status"`
	Runs     []ThreadRun      `json:"runs"`
	Request  GetThreadOptions `json:"request"`
}

// Client is the narrow boundary where the mock implementation can later be
// replaced by a real Pippit/Fornax client.
type Client interface {
	SubmitRun(ctx context.Context, opts SubmitRunOptions) (*SubmitRunResult, error)
	UploadFile(ctx context.Context, opts UploadFileOptions) (*UploadFileResult, error)
	GetThread(ctx context.Context, opts GetThreadOptions) (*GetThreadResult, error)
}

// MockClient returns deterministic IDs so the demo chain is easy to test.
type MockClient struct{}

func (MockClient) SubmitRun(ctx context.Context, opts SubmitRunOptions) (*SubmitRunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := opts.Agent + "\x00" + opts.Title + "\x00" + opts.Author + "\x00" + opts.Prompt
	id := stableID(key, 12)
	return &SubmitRunResult{
		Scene:     "novel",
		ThreadID:  "thread_mock_" + id[:6],
		RunID:     "run_mock_" + id[6:],
		Status:    "submitted",
		Submitted: time.Now().UTC().Format(time.RFC3339),
		Request:   opts,
	}, nil
}

func (MockClient) UploadFile(ctx context.Context, opts UploadFileOptions) (*UploadFileResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := opts.Purpose + "\x00" + opts.FileName + "\x00" + opts.Path
	id := stableID(key, 10)
	return &UploadFileResult{
		Scene:    "novel",
		FileID:   "file_mock_" + id,
		Status:   "uploaded",
		Uploaded: time.Now().UTC().Format(time.RFC3339),
		Request:  opts,
	}, nil
}

func (MockClient) GetThread(ctx context.Context, opts GetThreadOptions) (*GetThreadResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runID := "run_mock_" + stableID(opts.ThreadID, 8)
	return &GetThreadResult{
		Scene:    "novel",
		ThreadID: opts.ThreadID,
		Status:   "active",
		Runs: []ThreadRun{
			{RunID: runID, Status: "submitted", Scene: "novel"},
		},
		Request: opts,
	}, nil
}

func stableID(key string, n int) string {
	sum := sha1.Sum([]byte(key))
	id := hex.EncodeToString(sum[:])
	if n > len(id) {
		return id
	}
	return id[:n]
}
