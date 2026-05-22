package short_drama

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
)

// MockClient returns deterministic IDs so the demo chain is easy to test.
type MockClient struct{}

func (MockClient) SubmitRun(ctx context.Context, opts SubmitRunOptions) (*SubmitRunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := opts.ThreadID + "\x00" + opts.Message
	id := stableIDMock(key, 12)
	return &SubmitRunResult{
		ThreadID:      "thread_mock_" + id[:6],
		RunID:         "run_mock_" + id[6:],
		WebThreadLink: "https://xyq.jianying.com/mock/thread_mock_" + id[:6],
	}, nil
}

func (MockClient) GetThread(ctx context.Context, opts GetThreadOptions) (*GetThreadResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &GetThreadResult{
		Messages: []*ThreadEntry{
			{
				ID:      "message_mock_" + stableIDMock(opts.ThreadID, 8),
				Role:    "assistant",
				Content: []any{"mock thread message"},
			},
		},
	}, nil
}

func stableIDMock(key string, n int) string {
	sum := sha1.Sum([]byte(key))
	id := hex.EncodeToString(sum[:])
	if n > len(id) {
		return id
	}
	return id[:n]
}
