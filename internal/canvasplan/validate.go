package canvasplan

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func NormalizePlan(input Plan) (Plan, error) {
	plan := input
	plan.Schema = strings.TrimSpace(plan.Schema)
	plan.Title = strings.TrimSpace(plan.Title)
	plan.Source.Provider = strings.TrimSpace(plan.Source.Provider)
	plan.Source.ProjectID = strings.TrimSpace(plan.Source.ProjectID)
	plan.Source.Fingerprint = strings.TrimSpace(plan.Source.Fingerprint)
	if plan.Schema != PlanSchema {
		return Plan{}, fmt.Errorf("unsupported CanvasPlan schema %q", plan.Schema)
	}
	if plan.Title == "" || len([]rune(plan.Title)) > 50 {
		return Plan{}, fmt.Errorf("CanvasPlan title must contain 1 to 50 characters")
	}
	if plan.Source.Provider == "" || plan.Source.ProjectID == "" || plan.Source.Fingerprint == "" {
		return Plan{}, fmt.Errorf("CanvasPlan source provider, project_id, and fingerprint are required")
	}
	if len(plan.Nodes) == 0 {
		return Plan{}, fmt.Errorf("CanvasPlan must contain at least one business node")
	}

	mediaByID := make(map[string]MediaRequirement, len(plan.RequiredMedia))
	for index := range plan.RequiredMedia {
		media := &plan.RequiredMedia[index]
		media.LogicalID = strings.TrimSpace(media.LogicalID)
		media.SourceNodeID = strings.TrimSpace(media.SourceNodeID)
		media.FileName = strings.TrimSpace(media.FileName)
		media.MediaType = strings.ToLower(strings.TrimSpace(media.MediaType))
		media.URL = strings.TrimSpace(media.URL)
		media.LocalPath = strings.TrimSpace(media.LocalPath)
		media.SHA256 = strings.ToLower(strings.TrimSpace(media.SHA256))
		media.Metadata.Extension = strings.ToLower(strings.TrimSpace(media.Metadata.Extension))
		media.Metadata.MimeType = strings.TrimSpace(media.Metadata.MimeType)
		if err := validateLogicalID(media.LogicalID, fmt.Sprintf("required_media[%d].logical_id", index)); err != nil {
			return Plan{}, err
		}
		if media.SourceNodeID == "" || media.FileName == "" {
			return Plan{}, fmt.Errorf("required_media[%d] source_node_id and file_name are required", index)
		}
		if media.MediaType != "video" && media.MediaType != "audio" && media.MediaType != "image" {
			return Plan{}, fmt.Errorf("required_media[%d].media_type must be video, audio, or image", index)
		}
		if (media.URL == "") == (media.LocalPath == "") {
			return Plan{}, fmt.Errorf("required_media[%d] must define exactly one of url or local_path", index)
		}
		if media.URL != "" {
			parsed, err := url.Parse(media.URL)
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
				return Plan{}, fmt.Errorf("required_media[%d].url must be an absolute HTTPS URL", index)
			}
		}
		if media.LocalPath != "" {
			cleaned := path.Clean(media.LocalPath)
			if strings.Contains(media.LocalPath, `\`) || cleaned != media.LocalPath || strings.HasPrefix(cleaned, "/") || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
				return Plan{}, fmt.Errorf("required_media[%d].local_path must be a bundle-relative POSIX path", index)
			}
			media.LocalPath = cleaned
			if !sha256Pattern.MatchString(media.SHA256) {
				return Plan{}, fmt.Errorf("required_media[%d].sha256 must contain 64 lowercase hexadecimal characters for local_path", index)
			}
			if media.Metadata.ByteSize == nil || *media.Metadata.ByteSize <= 0 {
				return Plan{}, fmt.Errorf("required_media[%d].metadata.byte_size must be positive for local_path", index)
			}
		} else if media.SHA256 != "" && !sha256Pattern.MatchString(media.SHA256) {
			return Plan{}, fmt.Errorf("required_media[%d].sha256 must contain 64 lowercase hexadecimal characters", index)
		}
		if err := validateMediaMetadata(media.Metadata, index); err != nil {
			return Plan{}, err
		}
		if _, duplicate := mediaByID[media.LogicalID]; duplicate {
			return Plan{}, fmt.Errorf("duplicate required media logical_id %q", media.LogicalID)
		}
		mediaByID[media.LogicalID] = *media
	}

	nodesByID := make(map[string]Node, len(plan.Nodes))
	allLogicalIDs := make(map[string]struct{}, len(plan.Nodes)+len(plan.Groups))
	for index := range plan.Nodes {
		node := &plan.Nodes[index]
		normalizeNode(node)
		if err := validateLogicalID(node.LogicalID, fmt.Sprintf("nodes[%d].logical_id", index)); err != nil {
			return Plan{}, err
		}
		if node.SourceNodeID == "" || node.Title == "" {
			return Plan{}, fmt.Errorf("nodes[%d] source_node_id and title are required", index)
		}
		if err := validateGeometry(node.Position, node.Size, fmt.Sprintf("nodes[%d]", index)); err != nil {
			return Plan{}, err
		}
		if _, duplicate := allLogicalIDs[node.LogicalID]; duplicate {
			return Plan{}, fmt.Errorf("duplicate node logical_id %q", node.LogicalID)
		}
		allLogicalIDs[node.LogicalID] = struct{}{}
		if err := validateNodeContract(*node, mediaByID); err != nil {
			return Plan{}, fmt.Errorf("nodes[%d]: %w", index, err)
		}
		nodesByID[node.LogicalID] = *node
	}

	groupsByID := make(map[string]Group, len(plan.Groups))
	for index := range plan.Groups {
		group := &plan.Groups[index]
		group.LogicalID = strings.TrimSpace(group.LogicalID)
		group.SourceNodeID = strings.TrimSpace(group.SourceNodeID)
		group.Title = strings.TrimSpace(group.Title)
		if err := validateLogicalID(group.LogicalID, fmt.Sprintf("groups[%d].logical_id", index)); err != nil {
			return Plan{}, err
		}
		if group.SourceNodeID == "" || group.Title == "" {
			return Plan{}, fmt.Errorf("groups[%d] source_node_id and title are required", index)
		}
		if err := validateGeometry(group.Position, group.Size, fmt.Sprintf("groups[%d]", index)); err != nil {
			return Plan{}, err
		}
		if _, duplicate := allLogicalIDs[group.LogicalID]; duplicate {
			return Plan{}, fmt.Errorf("duplicate node/group logical_id %q", group.LogicalID)
		}
		allLogicalIDs[group.LogicalID] = struct{}{}
		seenChildren := make(map[string]struct{}, len(group.ChildLogicalIDs))
		for childIndex, childID := range group.ChildLogicalIDs {
			childID = strings.TrimSpace(childID)
			group.ChildLogicalIDs[childIndex] = childID
			if err := validateLogicalID(childID, fmt.Sprintf("groups[%d].child_logical_ids[%d]", index, childIndex)); err != nil {
				return Plan{}, err
			}
			if childID == group.LogicalID {
				return Plan{}, fmt.Errorf("group %q cannot contain itself", group.LogicalID)
			}
			if _, duplicate := seenChildren[childID]; duplicate {
				return Plan{}, fmt.Errorf("group %q contains duplicate child %q", group.LogicalID, childID)
			}
			seenChildren[childID] = struct{}{}
		}
		groupsByID[group.LogicalID] = *group
	}
	if err := validateGroupGraph(plan.Nodes, plan.Groups, nodesByID, groupsByID); err != nil {
		return Plan{}, err
	}

	seenEdges := make(map[string]struct{}, len(plan.Edges))
	for index := range plan.Edges {
		edge := &plan.Edges[index]
		edge.LogicalID = strings.TrimSpace(edge.LogicalID)
		edge.SourceEdgeID = strings.TrimSpace(edge.SourceEdgeID)
		edge.Type = strings.ToLower(strings.TrimSpace(edge.Type))
		edge.SourceNodeLogicalID = strings.TrimSpace(edge.SourceNodeLogicalID)
		edge.TargetNodeLogicalID = strings.TrimSpace(edge.TargetNodeLogicalID)
		edge.SourceHandle = strings.TrimSpace(edge.SourceHandle)
		edge.TargetHandle = strings.TrimSpace(edge.TargetHandle)
		if err := validateLogicalID(edge.LogicalID, fmt.Sprintf("edges[%d].logical_id", index)); err != nil {
			return Plan{}, err
		}
		if _, duplicate := seenEdges[edge.LogicalID]; duplicate {
			return Plan{}, fmt.Errorf("duplicate edge logical_id %q", edge.LogicalID)
		}
		seenEdges[edge.LogicalID] = struct{}{}
		if edge.SourceEdgeID == "" || edge.Type != "reference" || edge.SourceHandle == "" || edge.TargetHandle == "" {
			return Plan{}, fmt.Errorf("edges[%d] requires source_edge_id, reference type, and handles", index)
		}
		if _, ok := nodesByID[edge.SourceNodeLogicalID]; !ok {
			return Plan{}, fmt.Errorf("edge %q references missing source node %q", edge.LogicalID, edge.SourceNodeLogicalID)
		}
		if _, ok := nodesByID[edge.TargetNodeLogicalID]; !ok {
			return Plan{}, fmt.Errorf("edge %q references missing target node %q", edge.LogicalID, edge.TargetNodeLogicalID)
		}
	}

	usedMedia := make(map[string]struct{})
	hasPlaceholder := false
	for _, node := range plan.Nodes {
		if node.MediaLogicalID != "" {
			usedMedia[node.MediaLogicalID] = struct{}{}
		}
		if node.Kind == "image-placeholder" || node.Kind == "video-placeholder" {
			hasPlaceholder = true
		}
		for _, inputID := range node.InputNodeLogicalIDs {
			input, ok := nodesByID[inputID]
			if !ok || input.Kind != "video" {
				return Plan{}, fmt.Errorf("composite node %q input %q must reference a video node", node.LogicalID, inputID)
			}
		}
	}
	if len(usedMedia) != len(mediaByID) {
		return Plan{}, fmt.Errorf("CanvasPlan required_media must exactly match video/audio node media references")
	}
	for mediaID := range mediaByID {
		if _, used := usedMedia[mediaID]; !used {
			return Plan{}, fmt.Errorf("required media %q is not referenced by a node", mediaID)
		}
	}
	for index, degradation := range plan.Degradations {
		if len(degradation) == 0 || !json.Valid(degradation) {
			return Plan{}, fmt.Errorf("degradations[%d] must be valid JSON", index)
		}
		var record map[string]json.RawMessage
		if err := json.Unmarshal(degradation, &record); err != nil || record == nil {
			return Plan{}, fmt.Errorf("degradations[%d] must be a JSON object", index)
		}
	}
	if hasPlaceholder && len(plan.Degradations) == 0 {
		return Plan{}, fmt.Errorf("CanvasPlan placeholders require an explicit degradation record")
	}
	return plan, nil
}

func NormalizeResolvedMedia(input ResolvedMediaSet) (ResolvedMediaSet, error) {
	resolved := input
	resolved.Schema = strings.TrimSpace(resolved.Schema)
	if resolved.Schema != ResolvedMediaSchema {
		return ResolvedMediaSet{}, fmt.Errorf("unsupported resolved media schema %q", resolved.Schema)
	}
	seenLogical := make(map[string]struct{}, len(resolved.Media))
	for index := range resolved.Media {
		item := &resolved.Media[index]
		item.LogicalID = strings.TrimSpace(item.LogicalID)
		item.MediaType = strings.ToLower(strings.TrimSpace(item.MediaType))
		item.AssetID = strings.TrimSpace(item.AssetID)
		item.PippitAssetID = strings.TrimSpace(item.PippitAssetID)
		if err := validateLogicalID(item.LogicalID, fmt.Sprintf("media[%d].logical_id", index)); err != nil {
			return ResolvedMediaSet{}, err
		}
		if item.MediaType != "video" && item.MediaType != "audio" && item.MediaType != "image" {
			return ResolvedMediaSet{}, fmt.Errorf("media[%d].media_type must be video, audio, or image", index)
		}
		if item.AssetID == "" || item.PippitAssetID == "" {
			return ResolvedMediaSet{}, fmt.Errorf("media[%d] asset_id and pippit_asset_id are required JSON strings", index)
		}
		if _, duplicate := seenLogical[item.LogicalID]; duplicate {
			return ResolvedMediaSet{}, fmt.Errorf("duplicate resolved media logical_id %q", item.LogicalID)
		}
		seenLogical[item.LogicalID] = struct{}{}
	}
	sort.Slice(resolved.Media, func(i, j int) bool { return resolved.Media[i].LogicalID < resolved.Media[j].LogicalID })
	return resolved, nil
}

func ValidateResolution(plan Plan, resolved ResolvedMediaSet) error {
	requirements := make(map[string]MediaRequirement, len(plan.RequiredMedia))
	for _, item := range plan.RequiredMedia {
		requirements[item.LogicalID] = item
	}
	if len(requirements) != len(resolved.Media) {
		return fmt.Errorf("resolved media count %d does not match required media count %d", len(resolved.Media), len(requirements))
	}
	for _, item := range resolved.Media {
		requirement, ok := requirements[item.LogicalID]
		if !ok {
			return fmt.Errorf("resolved media %q is not required by CanvasPlan", item.LogicalID)
		}
		if requirement.MediaType != item.MediaType {
			return fmt.Errorf("resolved media %q type %q does not match required type %q", item.LogicalID, item.MediaType, requirement.MediaType)
		}
	}
	return nil
}

func normalizeNode(node *Node) {
	node.LogicalID = strings.TrimSpace(node.LogicalID)
	node.SourceNodeID = strings.TrimSpace(node.SourceNodeID)
	node.Title = strings.TrimSpace(node.Title)
	node.ParentGroupLogicalID = strings.TrimSpace(node.ParentGroupLogicalID)
	node.Kind = strings.ToLower(strings.TrimSpace(node.Kind))
	node.TargetType = strings.TrimSpace(node.TargetType)
	node.MediaLogicalID = strings.TrimSpace(node.MediaLogicalID)
	node.Variant = strings.TrimSpace(node.Variant)
	for index := range node.InputNodeLogicalIDs {
		node.InputNodeLogicalIDs[index] = strings.TrimSpace(node.InputNodeLogicalIDs[index])
	}
}

func validateNodeContract(node Node, mediaByID map[string]MediaRequirement) error {
	switch node.Kind {
	case "video", "audio", "image":
		expectedTarget := "biz/video"
		if node.Kind == "audio" {
			expectedTarget = "biz/audio"
		} else if node.Kind == "image" {
			expectedTarget = "biz/image"
		}
		if node.TargetType != expectedTarget {
			return fmt.Errorf("%s node target_type must be %s", node.Kind, expectedTarget)
		}
		media, ok := mediaByID[node.MediaLogicalID]
		if !ok || media.MediaType != node.Kind {
			return fmt.Errorf("%s node must reference matching required_media", node.Kind)
		}
		if node.Variant != "" || len(node.InputNodeLogicalIDs) != 0 {
			return fmt.Errorf("uploaded media node cannot define composite fields")
		}
	case "image-placeholder", "video-placeholder":
		expectedTarget := "biz/image"
		if node.Kind == "video-placeholder" {
			expectedTarget = "biz/video"
		}
		if node.TargetType != expectedTarget {
			return fmt.Errorf("%s node target_type must be %s", node.Kind, expectedTarget)
		}
		if node.MediaLogicalID != "" || node.Variant != "" || len(node.InputNodeLogicalIDs) != 0 {
			return fmt.Errorf("placeholder node cannot define media or composite fields")
		}
	case "video-composite":
		if node.TargetType != "biz/video" || node.Variant != "video-composite" {
			return fmt.Errorf("video-composite target_type and variant are invalid")
		}
		if node.MediaLogicalID != "" {
			return fmt.Errorf("video-composite cannot reference required_media")
		}
	default:
		return fmt.Errorf("unsupported node kind %q", node.Kind)
	}
	return nil
}

func validateMediaMetadata(metadata MediaMetadata, index int) error {
	for name, value := range map[string]*int64{
		"byte_size":   metadata.ByteSize,
		"duration_ms": metadata.DurationMS,
		"height":      metadata.Height,
		"width":       metadata.Width,
	} {
		if value != nil && *value <= 0 {
			return fmt.Errorf("required_media[%d].metadata.%s must be positive when present", index, name)
		}
	}
	return nil
}

func validateGeometry(position Position, size Size, field string) error {
	if math.IsNaN(position.X) || math.IsInf(position.X, 0) || math.IsNaN(position.Y) || math.IsInf(position.Y, 0) {
		return fmt.Errorf("%s position must be finite", field)
	}
	if math.IsNaN(size.Width) || math.IsInf(size.Width, 0) || math.IsNaN(size.Height) || math.IsInf(size.Height, 0) || size.Width <= 0 || size.Height <= 0 {
		return fmt.Errorf("%s size must be finite and positive", field)
	}
	return nil
}

func validateLogicalID(value, field string) error {
	if value == "" || len(value) > 256 || !strings.Contains(value, ":") {
		return fmt.Errorf("%s must be a namespaced logical ID", field)
	}
	for _, char := range value {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			return fmt.Errorf("%s must not contain whitespace or control characters", field)
		}
	}
	return nil
}

func validateGroupGraph(nodes []Node, groups []Group, nodesByID map[string]Node, groupsByID map[string]Group) error {
	parentByChild := make(map[string]string)
	for _, group := range groups {
		for _, childID := range group.ChildLogicalIDs {
			if _, node := nodesByID[childID]; !node {
				if _, nestedGroup := groupsByID[childID]; !nestedGroup {
					return fmt.Errorf("group %q references missing child %q", group.LogicalID, childID)
				}
			}
			if previous, duplicate := parentByChild[childID]; duplicate {
				return fmt.Errorf("child %q belongs to both groups %q and %q", childID, previous, group.LogicalID)
			}
			parentByChild[childID] = group.LogicalID
		}
	}
	for _, node := range nodes {
		if inferred := parentByChild[node.LogicalID]; inferred != node.ParentGroupLogicalID {
			return fmt.Errorf("node %q parent_group_logical_id %q does not match group children %q", node.LogicalID, node.ParentGroupLogicalID, inferred)
		}
	}
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string) error
	visit = func(groupID string) error {
		if visiting[groupID] {
			return fmt.Errorf("group hierarchy contains a cycle at %q", groupID)
		}
		if visited[groupID] {
			return nil
		}
		visiting[groupID] = true
		for _, childID := range groupsByID[groupID].ChildLogicalIDs {
			if _, ok := groupsByID[childID]; ok {
				if err := visit(childID); err != nil {
					return err
				}
			}
		}
		visiting[groupID] = false
		visited[groupID] = true
		return nil
	}
	for groupID := range groupsByID {
		if err := visit(groupID); err != nil {
			return err
		}
	}
	return nil
}
