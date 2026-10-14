package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

var canvasMediaAssetTypes = map[string]int{"image": 1, "video": 2, "audio": 3}

func mediaIdentityError(nodeID, field, reason string) error {
	return fmt.Errorf("INVALID_MEDIA_IDENTITY asset %q at %s: %s", nodeID, field, reason)
}

func identityField(values map[string]json.RawMessage, name string) (json.RawMessage, error) {
	var result json.RawMessage
	for key, value := range values {
		if !strings.EqualFold(key, name) {
			continue
		}
		if result != nil {
			return nil, fmt.Errorf("duplicate %s fields are ambiguous", name)
		}
		result = value
	}
	return result, nil
}

func identityString(values map[string]json.RawMessage, name string) (string, error) {
	raw, err := identityField(values, name)
	if err != nil {
		return "", err
	}
	if raw == nil || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a JSON string", name)
	}
	return strings.TrimSpace(value), nil
}

func mediaSection(raw json.RawMessage, kind string) (map[string]json.RawMessage, error) {
	var asset map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asset); err != nil {
		return nil, err
	}
	// Native AgentAsset type, not preview sections: videos may also contain Image.
	expectedType := canvasMediaAssetTypes[kind]
	if typeRaw, ok := rawField(asset, "AssetType", "asset_type"); ok {
		var actual int
		if err := json.Unmarshal(typeRaw, &actual); err != nil || actual != expectedType {
			return nil, fmt.Errorf("expected %s material", kind)
		}
	}
	sectionRaw, ok := rawField(asset, strings.ToUpper(kind[:1])+kind[1:], kind)
	if !ok {
		return nil, fmt.Errorf("expected %s material", kind)
	}
	var section map[string]json.RawMessage
	if err := json.Unmarshal(sectionRaw, &section); err != nil || section == nil {
		return nil, fmt.Errorf("invalid %s material section", kind)
	}
	return section, nil
}

func cloudMediaID(raw json.RawMessage, kind string) (string, error) {
	section, err := mediaSection(raw, kind)
	if err != nil {
		return "", err
	}
	id, err := optionalStringField(section, "asset_id", "assetId", "AssetID")
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("media ID is not ready; query the material again")
	}
	pippitID, err := assetIDFromRaw(raw)
	if err != nil {
		return "", err
	}
	if id == pippitID {
		return "", fmt.Errorf("cloud media ID cannot be the Pippit material ID")
	}
	return id, nil
}

func touchesMediaIdentity(path string) bool {
	switch strings.ToLower(path) {
	case "", "/type", "/content", "/content/assetid", "/content/pippitassetid":
		return true
	}
	return false
}

func decodeNodeAsset(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	textRaw, ok := rawField(record, "TextInfo", "Text", "textInfo", "text")
	if !ok {
		return nil, fmt.Errorf("node asset has no inline text content")
	}
	var text map[string]json.RawMessage
	if err := json.Unmarshal(textRaw, &text); err != nil {
		return nil, err
	}
	content, ok := rawField(text, "Content", "content")
	if !ok {
		return nil, fmt.Errorf("node asset has no inline text content")
	}
	var value string
	if json.Unmarshal(content, &value) == nil {
		content = json.RawMessage(value)
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(content, &node); err != nil || node == nil {
		return nil, fmt.Errorf("invalid node content")
	}
	return node, nil
}

// Only identity paths and their parents are projected. Other fields stay in the
// original request, so validation never reconstructs or rewrites persisted data.
func projectIdentityPatch(node map[string]json.RawMessage, patch PatchEntry) (map[string]json.RawMessage, error) {
	if patch.Path == "" {
		if patch.Op == "remove" {
			return nil, nil
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(patch.Value, &value); err != nil {
			return nil, err
		}
		return value, nil
	}
	if node == nil {
		return nil, fmt.Errorf("cannot validate a patch without its node base")
	}
	parts := strings.Split(strings.TrimPrefix(patch.Path, "/"), "/")
	key := parts[0]
	if len(parts) == 1 {
		if patch.Op == "remove" {
			delete(node, key)
		} else {
			node[key] = patch.Value
		}
		return node, nil
	}
	var content map[string]json.RawMessage
	if err := json.Unmarshal(node[key], &content); err != nil || content == nil {
		return nil, fmt.Errorf("cannot validate a media identity without its content base")
	}
	if patch.Op == "remove" {
		delete(content, parts[1])
	} else {
		content[parts[1]] = patch.Value
	}
	value, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	node[key] = value
	return node, nil
}

func readNodeIdentity(node map[string]json.RawMessage) (kind, pippitID, mediaID string, err error) {
	if node == nil {
		return
	}
	kind, err = identityString(node, "type")
	if err != nil {
		return
	}
	switch kind {
	case "biz/image", "biz/video", "biz/audio":
		kind = strings.TrimPrefix(kind, "biz/")
	default:
		kind = ""
		return
	}
	contentRaw, fieldErr := identityField(node, "content")
	if fieldErr != nil {
		err = fieldErr
		return
	}
	var content map[string]json.RawMessage
	if err = json.Unmarshal(contentRaw, &content); err != nil {
		return
	}
	pippitID, err = identityString(content, "pippitAssetId")
	if err != nil {
		return
	}
	mediaID, err = identityString(content, "assetId")
	return
}

func validateApplyMediaIdentities(ctx context.Context, request ApplyRequest, runner *common.Runner) error {
	patches := request.Transactions[0].Patches
	affected := make(map[string]bool)
	var ordered []string
	var toQuery []string
	for _, patch := range patches {
		if touchesMediaIdentity(patch.Path) && !affected[patch.AssetID] {
			affected[patch.AssetID] = true
			ordered = append(ordered, patch.AssetID)
			if patch.Path != "" {
				toQuery = append(toQuery, patch.AssetID)
			}
		}
	}
	nodes := make(map[string]map[string]json.RawMessage)
	type identity struct{ nodeID, kind, pippitID, mediaID string }
	previous := make(map[string]identity)
	if len(toQuery) > 0 {
		result, err := queryAssets(ctx, toQuery, true, runner)
		if err != nil {
			return fmt.Errorf("canvas media identity preflight failed: %w", err)
		}
		for _, raw := range result.Assets {
			id, _ := assetIDFromRaw(raw)
			node, err := decodeNodeAsset(raw)
			if err != nil {
				return mediaIdentityError(id, "/content", err.Error())
			}
			nodes[id] = node
			kind, pippitID, mediaID, readErr := readNodeIdentity(node)
			if readErr == nil {
				previous[id] = identity{id, kind, pippitID, mediaID}
			}
		}
	}
	for _, patch := range patches {
		if !affected[patch.AssetID] || !touchesMediaIdentity(patch.Path) {
			continue
		}
		node, err := projectIdentityPatch(nodes[patch.AssetID], patch)
		if err != nil {
			return mediaIdentityError(patch.AssetID, patch.Path, err.Error())
		}
		nodes[patch.AssetID] = node
	}
	var identities []identity
	var materialIDs []string
	seen := make(map[string]bool)
	for _, nodeID := range ordered {
		kind, pippitID, mediaID, err := readNodeIdentity(nodes[nodeID])
		if err != nil {
			return mediaIdentityError(nodeID, "/content", err.Error())
		}
		if kind == "" || (pippitID == "" && mediaID == "") {
			continue
		}
		if old, ok := previous[nodeID]; ok && old.kind == kind && old.pippitID == pippitID && old.mediaID == mediaID {
			continue
		}
		if mediaID == nodeID || (mediaID != "" && mediaID == pippitID) {
			return mediaIdentityError(nodeID, "/content/assetId", "expected cloud media ID, received Pippit material or Canvas node ID")
		}
		if pippitID == "" {
			return mediaIdentityError(nodeID, "/content/pippitAssetId", "Pippit material ID is required to verify the media")
		}
		if mediaID == "" {
			return mediaIdentityError(nodeID, "/content/assetId", "use a semantic media command to resolve pippitAssetId")
		}
		identities = append(identities, identity{nodeID, kind, pippitID, mediaID})
		if !seen[pippitID] {
			materialIDs = append(materialIDs, pippitID)
			seen[pippitID] = true
		}
	}
	if len(materialIDs) == 0 {
		return nil
	}
	materials, err := queryAssets(ctx, materialIDs, true, runner)
	if err != nil {
		return fmt.Errorf("canvas media identity preflight failed: %w", err)
	}
	byID := make(map[string]json.RawMessage)
	for _, raw := range materials.Assets {
		id, _ := assetIDFromRaw(raw)
		byID[id] = raw
	}
	for _, item := range identities {
		expected, err := cloudMediaID(byID[item.pippitID], item.kind)
		if err != nil {
			return mediaIdentityError(item.nodeID, "/content/assetId", err.Error())
		}
		if expected != item.mediaID {
			return mediaIdentityError(item.nodeID, "/content/assetId", "media ID does not belong to the Pippit material")
		}
	}
	return nil
}
