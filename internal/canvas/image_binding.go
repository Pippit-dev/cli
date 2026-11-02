package canvas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

var imageSubjectFields = []string{"appearanceinfo", "sourceinfos"}

func isImageSubjectField(field string) bool {
	name := strings.ToLower(field)
	return name == imageSubjectFields[0] || name == imageSubjectFields[1]
}

func nodeBindingContent(node map[string]json.RawMessage) map[string]json.RawMessage {
	raw, _ := identityField(node, "content")
	var content map[string]json.RawMessage
	_ = json.Unmarshal(raw, &content)
	return content
}

func decodeBindingValue(raw json.RawMessage) any {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	_ = decoder.Decode(&value)
	return value
}

func hasSubjectBinding(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		return len(value) > 0
	case map[string]any:
		return len(value) > 0
	default:
		return true
	}
}

func replacesImageSubjectBinding(patch PatchEntry) bool {
	if patch.Path != "" || patch.Op != "replace" {
		return false
	}
	var node map[string]json.RawMessage
	if json.Unmarshal(patch.Value, &node) != nil {
		return false
	}
	kind, _ := identityString(node, "type")
	if kind != "biz/image" {
		return false
	}
	for field, raw := range nodeBindingContent(node) {
		if isImageSubjectField(field) && hasSubjectBinding(decodeBindingValue(raw)) {
			return true
		}
	}
	return false
}

// Legacy fields may be retained or cleared, but new image bindings use the
// image protocol. Do not infer a role/scene conversion from arbitrary content.
func validateImageSubjectBinding(nodeID string, node, previous map[string]json.RawMessage) error {
	content := nodeBindingContent(node)
	oldContent := nodeBindingContent(previous)
	oldType, _ := identityString(previous, "type")
	for field, raw := range content {
		if !isImageSubjectField(field) {
			continue
		}
		value := decodeBindingValue(raw)
		if !hasSubjectBinding(value) {
			continue
		}
		if oldType == "biz/image" && reflect.DeepEqual(value, decodeBindingValue(oldContent[field])) {
			continue
		}
		return mediaIdentityError(nodeID, "/content/"+field,
			"biz/image cannot bind role/scene content; use content.pippitAssetId for an image or a biz/role or biz/scene node")
	}
	return nil
}

// Project only the selected identity subtree, including nested subject fields.
// RFC 6902 array operations must not collapse a child patch into its parent.
func projectMediaIdentityValue(raw json.RawMessage, parts []string, patch PatchEntry) (json.RawMessage, error) {
	if len(parts) == 0 {
		return patch.Value, nil
	}
	value := bytes.TrimSpace(raw)
	if len(value) == 0 {
		return nil, fmt.Errorf("cannot validate a media identity without its content base")
	}
	key := parts[0]
	if value[0] == '{' {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil {
			return nil, err
		}
		if len(parts) == 1 && patch.Op == "remove" {
			delete(object, key)
		} else {
			next, err := projectMediaIdentityValue(object[key], parts[1:], patch)
			if err != nil {
				return nil, err
			}
			object[key] = next
		}
		return json.Marshal(object)
	}
	if value[0] != '[' {
		return nil, fmt.Errorf("cannot validate a nested identity patch on a scalar")
	}
	var array []json.RawMessage
	if err := json.Unmarshal(value, &array); err != nil {
		return nil, err
	}
	index, err := strconv.Atoi(key)
	if key == "-" && len(parts) == 1 && patch.Op == "add" {
		index, err = len(array), nil
	}
	if err != nil || index < 0 || index > len(array) || (len(key) > 1 && key[0] == '0') {
		return nil, fmt.Errorf("invalid media identity array index %q", key)
	}
	if len(parts) == 1 && patch.Op == "add" {
		array = append(array, nil)
		copy(array[index+1:], array[index:])
		array[index] = patch.Value
	} else {
		if index == len(array) {
			return nil, fmt.Errorf("media identity array index %q does not exist", key)
		}
		if len(parts) == 1 && patch.Op == "remove" {
			array = append(array[:index], array[index+1:]...)
		} else {
			next, err := projectMediaIdentityValue(array[index], parts[1:], patch)
			if err != nil {
				return nil, err
			}
			array[index] = next
		}
	}
	return json.Marshal(array)
}
