package short_drama

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// MockClient returns deterministic IDs so the demo chain is easy to test.
type MockClient struct{}

func (MockClient) SubmitRun(ctx context.Context, opts SubmitRunOptions) (*SubmitRunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := opts.ThreadID + "\x00" + opts.Message
	id := stableID(key, 12)
	return &SubmitRunResult{
		ThreadID:      "thread_mock_" + id[:6],
		RunID:         "run_mock_" + id[6:],
		WebThreadLink: "https://xyq.jianying.com/mock/thread_mock_" + id[:6],
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
	return &GetThreadResult{
		Messages: []*ThreadEntry{
			{
				ID:      "message_mock_" + stableID(opts.ThreadID, 8),
				Role:    "assistant",
				Content: []any{"mock thread message"},
			},
		},
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
