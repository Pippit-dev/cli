package novel

import (
	"context"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

func GetThread(ctx context.Context, opts GetThreadOptions, _ *common.Runner) (*GetThreadResult, error) {
	return MockClient{}.GetThread(ctx, opts)
}
