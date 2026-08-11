package canvasplan

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

func Materialize(
	inputPlan Plan,
	inputResolved ResolvedMediaSet,
	rootCanvasID string,
	nodeAssetIDs map[string]string,
) (*Document, error) {
	plan, err := NormalizePlan(inputPlan)
	if err != nil {
		return nil, err
	}
	resolved, err := NormalizeResolvedMedia(inputResolved)
	if err != nil {
		return nil, err
	}
	if err := ValidateResolution(plan, resolved); err != nil {
		return nil, err
	}
	rootCanvasID = strings.TrimSpace(rootCanvasID)
	if rootCanvasID == "" {
		return nil, fmt.Errorf("root canvas ID is required")
	}
	mapping, err := validateNodeAssetIDs(plan, resolved, rootCanvasID, nodeAssetIDs)
	if err != nil {
		return nil, err
	}
	resolvedByID := make(map[string]ResolvedMedia, len(resolved.Media))
	for _, item := range resolved.Media {
		resolvedByID[item.LogicalID] = item
	}
	mediaByID := make(map[string]MediaRequirement, len(plan.RequiredMedia))
	for _, item := range plan.RequiredMedia {
		mediaByID[item.LogicalID] = item
	}
	groupParent := groupParents(plan.Groups)

	rootNodes := make(map[string]any, len(plan.Nodes)+len(plan.Groups))
	assets := make(map[string]json.RawMessage, len(plan.Nodes)+1)
	for _, group := range plan.Groups {
		children := make([]string, 0, len(group.ChildLogicalIDs))
		for _, childID := range group.ChildLogicalIDs {
			if assigned, ok := mapping[childID]; ok {
				children = append(children, assigned)
			} else {
				children = append(children, childID)
			}
		}
		rootNodes[group.LogicalID] = map[string]any{
			"id":       group.LogicalID,
			"type":     "group",
			"x":        group.Position.X,
			"y":        group.Position.Y,
			"w":        group.Size.Width,
			"h":        group.Size.Height,
			"parentId": nullableString(groupParent[group.LogicalID]),
			"order":    group.Order,
			"data": map[string]any{
				"title":     group.Title,
				"collapsed": false,
			},
			"children": children,
		}
	}

	for _, node := range plan.Nodes {
		assetID := mapping[node.LogicalID]
		projectionData := map[string]any{"name": node.Title}
		if node.Kind == "audio" {
			projectionData = map[string]any{}
		}
		rootNodes[assetID] = map[string]any{
			"id":       assetID,
			"type":     node.TargetType,
			"x":        node.Position.X,
			"y":        node.Position.Y,
			"w":        node.Size.Width,
			"h":        node.Size.Height,
			"parentId": nullableString(node.ParentGroupLogicalID),
			"order":    node.Order,
			"data":     projectionData,
		}
		content, err := materializeNodeContent(node, mediaByID, resolvedByID, mapping)
		if err != nil {
			return nil, err
		}
		asset := map[string]any{
			"pippitAssetId": assetID,
			"type":          node.TargetType,
			"content":       content,
			"extra":         map[string]any{},
		}
		assets[assetID], err = marshalRaw(asset)
		if err != nil {
			return nil, fmt.Errorf("marshal node asset %q: %w", node.LogicalID, err)
		}
	}

	rootEdges := make(map[string]any, len(plan.Edges))
	for _, edge := range plan.Edges {
		rootEdges[edge.LogicalID] = map[string]any{
			"id":           edge.LogicalID,
			"type":         edge.Type,
			"source":       mapping[edge.SourceNodeLogicalID],
			"target":       mapping[edge.TargetNodeLogicalID],
			"sourceHandle": edge.SourceHandle,
			"targetHandle": edge.TargetHandle,
		}
	}
	bounds, err := globalBounds(plan, groupParent)
	if err != nil {
		return nil, err
	}
	rootAsset := map[string]any{
		"pippitAssetId": rootCanvasID,
		"type":          "canvas",
		"content": map[string]any{
			"metadata": map[string]any{
				"title":        plan.Title,
				"globalBounds": bounds,
			},
			"settings": map[string]any{
				"snap": map[string]any{
					"enabled":   true,
					"threshold": 8,
					"targets":   []string{"grid", "node"},
				},
				"grid": map[string]any{"size": 28, "visible": true},
			},
			"nodes": rootNodes,
			"edges": rootEdges,
		},
		"extra": map[string]any{},
	}
	assets[rootCanvasID], err = marshalRaw(rootAsset)
	if err != nil {
		return nil, fmt.Errorf("marshal root canvas asset: %w", err)
	}
	return &Document{Revision: 0, RootCanvasID: rootCanvasID, Assets: assets}, nil
}

func DocumentSHA256(document *Document) (string, error) {
	if document == nil {
		return "", fmt.Errorf("document is required")
	}
	return hashJSON(document)
}

func DocumentAssetSHA256(document *Document) (map[string]string, error) {
	if document == nil {
		return nil, fmt.Errorf("document is required")
	}
	result := make(map[string]string, len(document.Assets))
	for assetID, asset := range document.Assets {
		hash, err := hashRawJSON(asset)
		if err != nil {
			return nil, fmt.Errorf("hash asset %q: %w", assetID, err)
		}
		result[assetID] = hash
	}
	return result, nil
}

func DocumentAssetIDs(document *Document) []string {
	if document == nil {
		return nil
	}
	ids := make([]string, 0, len(document.Assets))
	for assetID := range document.Assets {
		if assetID != document.RootCanvasID {
			ids = append(ids, assetID)
		}
	}
	sort.Strings(ids)
	return append([]string{document.RootCanvasID}, ids...)
}

func validateNodeAssetIDs(plan Plan, resolved ResolvedMediaSet, rootID string, input map[string]string) (map[string]string, error) {
	if len(input) != len(plan.Nodes) {
		return nil, fmt.Errorf("node asset ID count %d does not match node count %d", len(input), len(plan.Nodes))
	}
	reserved := map[string]struct{}{rootID: {}}
	for _, item := range resolved.Media {
		reserved[item.PippitAssetID] = struct{}{}
	}
	for _, group := range plan.Groups {
		reserved[group.LogicalID] = struct{}{}
	}
	for _, edge := range plan.Edges {
		reserved[edge.LogicalID] = struct{}{}
	}
	result := make(map[string]string, len(input))
	for _, node := range plan.Nodes {
		assetID := strings.TrimSpace(input[node.LogicalID])
		if assetID == "" {
			return nil, fmt.Errorf("node %q has no allocated asset ID", node.LogicalID)
		}
		if _, collision := reserved[assetID]; collision {
			return nil, fmt.Errorf("allocated asset ID %q collides with another document identifier", assetID)
		}
		reserved[assetID] = struct{}{}
		result[node.LogicalID] = assetID
	}
	return result, nil
}

func materializeNodeContent(
	node Node,
	mediaByID map[string]MediaRequirement,
	resolvedByID map[string]ResolvedMedia,
	mapping map[string]string,
) (map[string]any, error) {
	switch node.Kind {
	case "video", "audio", "image":
		requirement := mediaByID[node.MediaLogicalID]
		resolved := resolvedByID[node.MediaLogicalID]
		if node.Kind == "audio" {
			content := map[string]any{
				"assetId":       resolved.AssetID,
				"generation":    map[string]any{"source": "uploaded"},
				"name":          node.Title,
				"pippitAssetId": resolved.PippitAssetID,
				"source":        "uploaded",
			}
			putInt64(content, "durationMs", requirement.Metadata.DurationMS)
			return content, nil
		}
		if node.Kind == "image" {
			metadata := map[string]any{
				"format": mediaFormat(requirement, "image"),
				"name":   node.Title,
			}
			putInt64(metadata, "height", requirement.Metadata.Height)
			putInt64(metadata, "size", requirement.Metadata.ByteSize)
			putInt64(metadata, "width", requirement.Metadata.Width)
			content := map[string]any{
				"assetId":       resolved.AssetID,
				"generation":    map[string]any{"source": "uploaded"},
				"metadata":      metadata,
				"name":          node.Title,
				"pippitAssetId": resolved.PippitAssetID,
				"source":        "uploaded",
				"sourceType":    4,
			}
			putInt64(content, "naturalHeight", requirement.Metadata.Height)
			putInt64(content, "naturalWidth", requirement.Metadata.Width)
			return content, nil
		}
		metadata := map[string]any{
			"format": mediaFormat(requirement, "video"),
			"name":   node.Title,
		}
		putInt64(metadata, "durationMS", requirement.Metadata.DurationMS)
		putInt64(metadata, "durationMs", requirement.Metadata.DurationMS)
		putInt64(metadata, "height", requirement.Metadata.Height)
		putInt64(metadata, "size", requirement.Metadata.ByteSize)
		putInt64(metadata, "width", requirement.Metadata.Width)
		content := map[string]any{
			"assetId":       resolved.AssetID,
			"caption":       node.Title,
			"metadata":      metadata,
			"name":          node.Title,
			"pippitAssetId": resolved.PippitAssetID,
			"playback":      map[string]any{"muted": true},
			"source":        "uploaded",
			"title":         node.Title,
		}
		if requirement.Metadata.DurationMS != nil {
			content["duration"] = float64(*requirement.Metadata.DurationMS) / 1000
		}
		putInt64(content, "durationMs", requirement.Metadata.DurationMS)
		putInt64(content, "frameHeight", requirement.Metadata.Height)
		putInt64(content, "frameWidth", requirement.Metadata.Width)
		putInt64(content, "height", requirement.Metadata.Height)
		putInt64(content, "width", requirement.Metadata.Width)
		return content, nil
	case "image-placeholder":
		return map[string]any{"caption": node.Title, "name": node.Title}, nil
	case "video-placeholder":
		return map[string]any{"caption": node.Title, "name": node.Title, "title": node.Title}, nil
	case "video-composite":
		references := make([]map[string]any, 0, len(node.InputNodeLogicalIDs))
		for _, inputID := range node.InputNodeLogicalIDs {
			assignedID := mapping[inputID]
			if assignedID == "" {
				return nil, fmt.Errorf("composite node %q input %q has no allocated ID", node.LogicalID, inputID)
			}
			references = append(references, map[string]any{
				"id":             assignedID,
				"type":           "video",
				"nodeAssetId":    assignedID,
				"sourceNodeId":   assignedID,
				"sourceNodeType": "biz/video",
			})
		}
		return map[string]any{
			"caption": "视频",
			"generation": map[string]any{
				"source":     "tool",
				"references": references,
			},
			"name":    node.Title,
			"variant": "video-composite",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported node kind %q", node.Kind)
	}
}

func mediaFormat(requirement MediaRequirement, fallback string) string {
	if value := strings.TrimSpace(requirement.Metadata.Extension); value != "" {
		return strings.ToLower(strings.TrimPrefix(value, "."))
	}
	if extensionIndex := strings.LastIndex(requirement.FileName, "."); extensionIndex >= 0 && extensionIndex < len(requirement.FileName)-1 {
		return strings.ToLower(requirement.FileName[extensionIndex+1:])
	}
	if slashIndex := strings.Index(requirement.Metadata.MimeType, "/"); slashIndex >= 0 && slashIndex < len(requirement.Metadata.MimeType)-1 {
		return strings.ToLower(requirement.Metadata.MimeType[slashIndex+1:])
	}
	return fallback
}

func globalBounds(plan Plan, groupParent map[string]string) (map[string]float64, error) {
	type item struct {
		position Position
		size     Size
	}
	items := make([]item, 0, len(plan.Nodes)+len(plan.Groups))
	for _, node := range plan.Nodes {
		if node.ParentGroupLogicalID == "" {
			items = append(items, item{position: node.Position, size: node.Size})
		}
	}
	for _, group := range plan.Groups {
		if groupParent[group.LogicalID] == "" {
			items = append(items, item{position: group.Position, size: group.Size})
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("CanvasPlan has no top-level layout items")
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, item := range items {
		minX = math.Min(minX, item.position.X)
		minY = math.Min(minY, item.position.Y)
		maxX = math.Max(maxX, item.position.X+item.size.Width)
		maxY = math.Max(maxY, item.position.Y+item.size.Height)
	}
	return map[string]float64{"minX": minX, "minY": minY, "maxX": maxX, "maxY": maxY}, nil
}

func groupParents(groups []Group) map[string]string {
	parents := make(map[string]string)
	groupIDs := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		groupIDs[group.LogicalID] = struct{}{}
	}
	for _, group := range groups {
		for _, childID := range group.ChildLogicalIDs {
			if _, isGroup := groupIDs[childID]; isGroup {
				parents[childID] = group.LogicalID
			}
		}
	}
	return parents
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func putInt64(target map[string]any, key string, value *int64) {
	if value != nil {
		target[key] = *value
	}
}

func marshalRaw(value any) (json.RawMessage, error) {
	payload, err := json.Marshal(value)
	return json.RawMessage(payload), err
}
