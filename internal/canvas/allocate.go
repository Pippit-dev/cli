package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const MaxAllocateCount = 5000

type AllocateResult struct {
	AssetIDs []string `json:"asset_ids"`
	LogID    string   `json:"log_id,omitempty"`
}

type allocateRequest struct {
	Count int            `json:"count"`
	Base  map[string]any `json:"Base"`
}

// Allocate reserves unique IDs for assets that a later canvas apply transaction creates.
func Allocate(ctx context.Context, count int, runner *common.Runner) (*AllocateResult, error) {
	client, err := runnerClient(runner, "canvas allocate")
	if err != nil {
		return nil, err
	}
	if count <= 0 || count > MaxAllocateCount {
		return nil, fmt.Errorf("canvas allocate count must be between 1 and %d", MaxAllocateCount)
	}
	var envelope responseEnvelope
	if err := client.SendRequest(ctx, AllocatePath, allocateRequest{Count: count, Base: base()}, &envelope); err != nil {
		return nil, fmt.Errorf("canvas allocate request failed: %w", err)
	}
	if err := envelope.validate("canvas allocate"); err != nil {
		return nil, err
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, common.NewLogIDError(fmt.Sprintf("canvas allocate returned invalid data: %v", err), envelope.LogID)
	}
	idsRaw, ok := rawField(data, "ids", "IDs", "asset_ids")
	if !ok {
		return nil, common.NewLogIDError("canvas allocate response is missing data.ids", envelope.LogID)
	}
	var ids []string
	if err := json.Unmarshal(idsRaw, &ids); err != nil {
		return nil, common.NewLogIDError("canvas allocate data.ids must contain JSON strings", envelope.LogID)
	}
	if len(ids) != count {
		return nil, common.NewLogIDError(
			fmt.Sprintf("canvas allocate returned %d ids, want %d", len(ids), count),
			envelope.LogID,
		)
	}
	seen := make(map[string]struct{}, len(ids))
	for index, id := range ids {
		ids[index] = strings.TrimSpace(id)
		if ids[index] == "" {
			return nil, common.NewLogIDError(fmt.Sprintf("canvas allocate returned empty id at index %d", index), envelope.LogID)
		}
		if _, duplicate := seen[ids[index]]; duplicate {
			return nil, common.NewLogIDError(fmt.Sprintf("canvas allocate returned duplicate id %q", ids[index]), envelope.LogID)
		}
		seen[ids[index]] = struct{}{}
	}
	return &AllocateResult{AssetIDs: ids, LogID: strings.TrimSpace(envelope.LogID)}, nil
}
