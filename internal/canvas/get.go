package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

type GetOptions struct {
	AssetIDs []string
}

type GetResult struct {
	RequestedAssetIDs []string          `json:"requested_asset_ids"`
	Assets            []json.RawMessage `json:"assets"`
	LogID             string            `json:"log_id,omitempty"`
}

type queryRequest struct {
	PippitAssetIDs []string       `json:"pippit_asset_ids"`
	Base           map[string]any `json:"Base"`
}

func Get(ctx context.Context, opts GetOptions, runner *common.Runner) (*GetResult, error) {
	assetIDs, err := normalizeAssetIDs(opts.AssetIDs)
	if err != nil {
		return nil, err
	}
	return queryAssets(ctx, assetIDs, true, runner)
}

func queryAssets(ctx context.Context, assetIDs []string, requireAll bool, runner *common.Runner) (*GetResult, error) {
	client, err := runnerClient(runner, "canvas get")
	if err != nil {
		return nil, err
	}
	var envelope responseEnvelope
	if err := client.SendRequest(ctx, QueryPath, queryRequest{PippitAssetIDs: assetIDs, Base: base()}, &envelope); err != nil {
		return nil, fmt.Errorf("canvas get request failed: %w", err)
	}
	if err := envelope.validate("canvas get"); err != nil {
		return nil, err
	}
	assets, err := assetsFromData(envelope.Data)
	if err != nil {
		return nil, common.NewLogIDError(fmt.Sprintf("canvas get returned invalid data: %v", err), envelope.LogID)
	}

	byID := make(map[string]json.RawMessage, len(assets))
	for _, asset := range assets {
		assetID, err := assetIDFromRaw(asset)
		if err != nil {
			return nil, common.NewLogIDError(fmt.Sprintf("canvas get returned invalid asset: %v", err), envelope.LogID)
		}
		if _, duplicate := byID[assetID]; duplicate {
			return nil, common.NewLogIDError(fmt.Sprintf("canvas get returned duplicate asset %q", assetID), envelope.LogID)
		}
		byID[assetID] = asset
	}

	ordered := make([]json.RawMessage, 0, len(assetIDs))
	missing := make([]string, 0)
	for _, assetID := range assetIDs {
		asset, ok := byID[assetID]
		if !ok {
			missing = append(missing, assetID)
			continue
		}
		ordered = append(ordered, asset)
	}
	if requireAll && len(missing) != 0 {
		return nil, common.NewLogIDError(
			fmt.Sprintf("canvas get did not return requested assets: %s", strings.Join(missing, ", ")),
			envelope.LogID,
		)
	}
	return &GetResult{
		RequestedAssetIDs: append([]string(nil), assetIDs...),
		Assets:            ordered,
		LogID:             strings.TrimSpace(envelope.LogID),
	}, nil
}

func normalizeAssetIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one canvas asset_id is required")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("canvas asset_id at index %d is empty", index)
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("canvas asset_id %q is duplicated", value)
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func assetsFromData(raw json.RawMessage) ([]json.RawMessage, error) {
	var data map[string]json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("data is missing")
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	assetsRaw, ok := rawField(data, "Assets", "assets")
	if !ok {
		return nil, fmt.Errorf("data.assets is missing")
	}
	var assets []json.RawMessage
	if err := json.Unmarshal(assetsRaw, &assets); err != nil {
		return nil, fmt.Errorf("decode data.assets: %w", err)
	}
	if assets == nil {
		assets = []json.RawMessage{}
	}
	return assets, nil
}

func assetIDFromRaw(raw json.RawMessage) (string, error) {
	var asset map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asset); err != nil {
		return "", err
	}
	value, ok := rawField(asset, "PippitAssetID", "pippit_asset_id", "pippitAssetId")
	if !ok {
		return "", fmt.Errorf("asset is missing pippit_asset_id")
	}
	var assetID string
	if err := json.Unmarshal(value, &assetID); err != nil {
		return "", fmt.Errorf("pippit_asset_id must be a JSON string")
	}
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return "", fmt.Errorf("pippit_asset_id is empty")
	}
	return assetID, nil
}

func rawField(values map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	return nil, false
}
