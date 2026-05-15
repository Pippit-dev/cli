package novel

import (
	"context"

	"github.com/Pippit-dev/pippit-cli/internal"
)

func GetThread(ctx context.Context, opts GetThreadOptions, _ *internal.Client) (*GetThreadResult, error) {
	return MockClient{}.GetThread(ctx, opts)
}
