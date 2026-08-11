package canvasplan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func VerifyDocument(document *Document, assets []json.RawMessage) Verification {
	verification := Verification{ExpectedAssetCount: len(document.Assets), ReturnedAssetCount: len(assets)}
	queried := make(map[string]json.RawMessage, len(assets))
	for _, asset := range assets {
		assetID, err := queriedAssetID(asset)
		if err == nil {
			queried[assetID] = asset
		}
	}
	for _, assetID := range DocumentAssetIDs(document) {
		queriedAsset, exists := queried[assetID]
		if !exists {
			verification.MissingAssetIDs = append(verification.MissingAssetIDs, assetID)
			continue
		}
		stored, err := queriedAssetContent(queriedAsset)
		if err != nil {
			verification.UnverifiableAssetIDs = append(verification.UnverifiableAssetIDs, assetID)
			continue
		}
		expectedHash, expectedErr := hashRawJSON(document.Assets[assetID])
		storedHash, storedErr := hashRawJSON(stored)
		if expectedErr != nil || storedErr != nil {
			verification.UnverifiableAssetIDs = append(verification.UnverifiableAssetIDs, assetID)
			continue
		}
		if expectedHash != storedHash {
			verification.MismatchedAssetIDs = append(verification.MismatchedAssetIDs, assetID)
		}
	}
	verification.Verified = len(verification.MissingAssetIDs) == 0 &&
		len(verification.UnverifiableAssetIDs) == 0 &&
		len(verification.MismatchedAssetIDs) == 0
	return verification
}

func queriedAssetID(raw json.RawMessage) (string, error) {
	asset, err := rawObject(raw)
	if err != nil {
		return "", err
	}
	value, ok := firstRawField(asset, "PippitAssetID", "pippit_asset_id", "pippitAssetId")
	if !ok {
		return "", fmt.Errorf("queried asset has no pippit_asset_id")
	}
	var assetID string
	if err := json.Unmarshal(value, &assetID); err != nil {
		return "", fmt.Errorf("queried pippit_asset_id must be a JSON string")
	}
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return "", fmt.Errorf("queried pippit_asset_id is empty")
	}
	return assetID, nil
}

func queriedAssetVersion(raw json.RawMessage) (int64, error) {
	asset, err := rawObject(raw)
	if err != nil {
		return 0, err
	}
	value, ok := firstRawField(asset, "version", "Version", "asset_version", "assetVersion")
	if !ok {
		return 0, fmt.Errorf("queried asset has no version")
	}
	var version int64
	if err := json.Unmarshal(value, &version); err != nil || version < 0 {
		return 0, fmt.Errorf("queried asset version must be a non-negative integer")
	}
	return version, nil
}

func queriedAssetContent(raw json.RawMessage) (json.RawMessage, error) {
	asset, err := rawObject(raw)
	if err != nil {
		return nil, err
	}
	textRaw, ok := firstRawField(asset, "TextInfo", "textInfo", "text")
	if !ok {
		return nil, fmt.Errorf("queried asset has no text content")
	}
	text, err := rawObject(textRaw)
	if err != nil {
		return nil, fmt.Errorf("decode queried text info: %w", err)
	}
	contentRaw, ok := firstRawField(text, "Content", "content")
	if !ok {
		return nil, fmt.Errorf("queried asset text has no content")
	}
	var encoded string
	if json.Unmarshal(contentRaw, &encoded) == nil {
		encoded = strings.TrimSpace(encoded)
		if encoded == "" || !json.Valid([]byte(encoded)) {
			return nil, fmt.Errorf("queried asset text content is not JSON")
		}
		return json.RawMessage(encoded), nil
	}
	if !json.Valid(contentRaw) || string(contentRaw) == "null" {
		return nil, fmt.Errorf("queried asset text content is not JSON")
	}
	return contentRaw, nil
}

func indexQueriedAssets(assets []json.RawMessage) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(assets))
	for _, asset := range assets {
		assetID, err := queriedAssetID(asset)
		if err != nil {
			return nil, err
		}
		if _, duplicate := result[assetID]; duplicate {
			return nil, fmt.Errorf("query returned duplicate asset %q", assetID)
		}
		result[assetID] = asset
	}
	return result, nil
}

func existingCompanionIDs(document *Document, queried map[string]json.RawMessage) []string {
	result := make([]string, 0)
	for assetID := range document.Assets {
		if assetID == document.RootCanvasID {
			continue
		}
		if _, exists := queried[assetID]; exists {
			result = append(result, assetID)
		}
	}
	sort.Strings(result)
	return result
}

func rawObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("JSON object is missing")
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("JSON object is missing")
	}
	return value, nil
}

func firstRawField(values map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	return nil, false
}
