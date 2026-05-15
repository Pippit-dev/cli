package novel

import (
	"context"

	"github.com/Pippit-dev/pippit-cli/internal"
)

func UploadFile(ctx context.Context, opts UploadFileOptions, _ *internal.Client) (*UploadFileResult, error) {
	return MockClient{}.UploadFile(ctx, opts)
}
