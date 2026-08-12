package canvas

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const (
	CreatePath   = "/api/biz/v1/skill/canvas/create"
	QueryPath    = "/api/biz/v1/skill/canvas/query"
	AllocatePath = "/api/biz/v1/skill/canvas/batch_generate_asset_id"
	ApplyPath    = "/api/biz/v1/skill/canvas/batch_patch_asset"
	UploadPath   = "/api/biz/v1/skill/upload_file"
)

const (
	SurfaceNovel        = "novel"
	StateCreating       = "creating"
	StateProcessing     = "processing"
	StateReady          = "ready"
	overviewPartSubtype = "biz/x_data_novel_script_overview"
)

type responseEnvelope struct {
	Ret    string          `json:"ret"`
	Errmsg string          `json:"errmsg"`
	LogID  string          `json:"log_id"`
	Data   json.RawMessage `json:"data"`
}

func (r responseEnvelope) validate(operation string) error {
	if r.Ret == "0" {
		return nil
	}
	message := strings.TrimSpace(r.Errmsg)
	if message == "" {
		message = "unknown error"
	}
	responseErr := common.NewLogIDError(
		fmt.Sprintf("%s failed: ret=%q errmsg=%s", operation, r.Ret, message),
		r.LogID,
	)
	if r.Ret == "1015" {
		// ret=1015 is the service's explicit credential rejection. Unlike a
		// transport failure or 5xx, it proves the business write was rejected.
		return fmt.Errorf("%w: %w", internal_auth.ErrCredentialRejected, responseErr)
	}
	return responseErr
}

func base() map[string]any {
	return map[string]any{}
}

func runnerClient(runner *common.Runner, operation string) (common.Client, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("%s client is missing", operation)
	}
	return runner.Client, nil
}

func pathWithProjectID(path, projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return path
	}
	query := url.Values{}
	query.Set("project_id", projectID)
	return path + "?" + query.Encode()
}
