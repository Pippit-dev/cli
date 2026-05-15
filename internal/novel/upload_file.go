package novel

import (
	"context"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

func UploadFile(ctx context.Context, opts UploadFileOptions, _ *common.Runner) (*UploadFileResult, error) {
	return MockClient{}.UploadFile(ctx, opts)
}
