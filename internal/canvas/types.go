package canvas

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

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
	return common.NewLogIDError(
		fmt.Sprintf("%s failed: ret=%q errmsg=%s", operation, r.Ret, message),
		r.LogID,
	)
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

func pathWithApplyQuery(path, projectID, commandName string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" && commandName == "" {
		return path, nil
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid canvas write URL: %w", err)
	}
	query := parsed.Query()
	if projectID != "" {
		query.Set("project_id", projectID)
	}
	if commandName != "" {
		query.Set("command", commandName)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
