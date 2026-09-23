package models

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

type dimensionConfig struct {
	Key          string  `json:"key"`
	Label        string  `json:"label"`
	Description  string  `json:"description"`
	Required     *bool   `json:"required_field"`
	DefaultValue *string `json:"default_value"`
	OptionList   []struct {
		Value    string `json:"value"`
		Disabled bool   `json:"disabled"`
	} `json:"option_list"`
	RangeConfig *struct {
		Min  *int64 `json:"min_value"`
		Max  *int64 `json:"max_value"`
		Step int64  `json:"step"`
	} `json:"range_config"`
	ActiveWhenAny json.RawMessage `json:"active_when_any"`
}

// Normalize only fields with an explicit CLI contract. RawMessage preserves
// unknown fields and large integers; the cached catalog is never mutated.
func describeModel(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	var source struct {
		Key       string  `json:"key"`
		Name      string  `json:"name"`
		Kind      string  `json:"kind"`
		Ratios    []int64 `json:"supported_ratio_list"`
		Default   *int64  `json:"default_ratio"`
		Parameter struct {
			Dimensions []*dimensionConfig `json:"dimensions"`
		} `json:"parameter_config"`
		Creation *struct {
			Modes []json.RawMessage `json:"modes"`
		} `json:"creation_mode_config"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, fmt.Errorf("模型参数配置无法解析，请 --refresh 重试或升级 CLI: %w", err)
	}
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		out[key] = value
	}
	delete(out, "config_key")
	delete(out, "is_default") // A server default does not authorize model selection.
	if source.Kind == "image" {
		// Billing maps and other Web-only metadata can contain wire model names.
		// Keep them in the raw catalog, not in host-facing command output.
		out = map[string]any{"name": strings.TrimSpace(source.Name), "kind": source.Kind}
		for _, key := range []string{"description", "parameter_config", "creation_mode_config"} {
			if value, exists := fields[key]; exists {
				out[key] = value
			}
		}
	}
	warnings := []string{}
	warn := func(message string) { warnings = append(warnings, message) }
	ratios := make([]any, 0, len(source.Ratios))
	for _, value := range source.Ratios {
		if value, ok := cliRatioValue(value, source.Kind); ok && !slices.Contains(ratios, value) {
			ratios = append(ratios, value)
		}
	}
	ratio := map[string]any{"options": ratios}
	if source.Default != nil {
		if value, ok := cliRatioValue(*source.Default, source.Kind); ok {
			if slices.Contains(ratios, value) {
				ratio["default"] = value
			} else {
				warn("默认比例不在可用比例中，未输出默认值；请 --refresh 重试")
			}
		}
	}
	if _, exists := fields["supported_ratio_list"]; exists || source.Default != nil {
		out["ratio"] = ratio
	}
	delete(out, "supported_ratio_list")
	delete(out, "default_ratio")

	seen := make(map[string]bool)
	for _, dimension := range source.Parameter.Dimensions {
		if dimension == nil {
			continue
		}
		if source.Kind == "image" {
			if dimension.Key != "resolution" && dimension.Key != "ratio" && dimension.Key != "effort" {
				continue
			}
		} else if dimension.Key != "resolution" && dimension.Key != "duration" {
			continue
		}
		if seen[dimension.Key] {
			return nil, fmt.Errorf("模型配置包含重复 %s 维度，请 --refresh 重试", dimension.Key)
		}
		seen[dimension.Key] = true
		out[dimension.Key] = describeDimension(dimension, source.Kind, warn)
	}
	if source.Kind == "image" {
		if imageRatio, ok := out["ratio"].(map[string]any); ok {
			if options, ok := imageRatio["options"].([]any); ok {
				labels := make(map[string]string, len(options))
				for _, option := range options {
					value := option.(int64)
					labels[strconv.FormatInt(value, 10)] = common.RatioValue(value)
				}
				imageRatio["option_labels"] = labels
			}
		}
	}
	// Image dimensions retain their complete source metadata and combination rules.
	// Video descriptions keep the existing compact representation.
	if parameterRaw, ok := fields["parameter_config"]; source.Kind != "image" && ok && string(parameterRaw) != "null" {
		var parameter map[string]json.RawMessage
		if err := json.Unmarshal(parameterRaw, &parameter); err != nil {
			return nil, err
		}
		var dimensions []json.RawMessage
		if err := json.Unmarshal(parameter["dimensions"], &dimensions); len(parameter["dimensions"]) != 0 && err != nil {
			return nil, err
		}
		remaining := []json.RawMessage{}
		for _, dimension := range dimensions {
			var key struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(dimension, &key); err != nil {
				return nil, err
			}
			if key.Key != "resolution" && key.Key != "duration" {
				remaining = append(remaining, dimension)
			}
		}
		delete(parameter, "dimensions")
		if len(remaining) > 0 {
			parameter["dimensions"], _ = json.Marshal(remaining)
		}
		delete(out, "parameter_config")
		if len(parameter) > 0 {
			out["parameter_config"] = parameter
		}
	}
	if seen["duration"] {
		delete(out, "supported_duration_list")
		delete(out, "default_duration_value")
	} else if _, exists := fields["supported_duration_list"]; exists {
		warn("服务端未提供 duration 参数维度；supported_duration_list 为旧时长枚举，不能直接当作 --duration 秒数")
	}

	limits := map[string]json.RawMessage{}
	for from, to := range map[string]string{
		"total_limit": "total_limit", "image_total_limit": "image_total_limit",
		"video_total_limit": "video_total_limit", "audio_total_limit": "audio_total_limit",
		"max_image_size": "max_image_size_bytes", "min_video_duration": "min_video_duration_ms",
		"max_video_duration": "max_video_duration_ms", "max_total_video_duration": "max_total_video_duration_ms",
	} {
		if value, exists := fields[from]; exists {
			limits[to] = value
			delete(out, from)
		}
	}
	if len(limits) > 0 {
		out["material_limits"] = limits
	}
	if source.Creation != nil && source.Kind != "image" {
		modes := make([]map[string]any, 0, len(source.Creation.Modes))
		for _, rawMode := range source.Creation.Modes {
			if string(rawMode) == "null" {
				continue
			}
			var modeFields map[string]json.RawMessage
			var mode struct {
				Key         string `json:"key"`
				RatioPolicy struct {
					ValueMode string `json:"value_mode"`
				} `json:"ratio_policy"`
			}
			if err := json.Unmarshal(rawMode, &modeFields); err != nil {
				return nil, err
			}
			if err := json.Unmarshal(rawMode, &mode); err != nil {
				return nil, err
			}
			entry := make(map[string]any, len(modeFields)+2)
			for key, value := range modeFields {
				entry[key] = value
			}
			switch mode.Key {
			case "text_to_video", "reference_generation":
				entry["generate_type"] = 0
			case "first_last_frame":
				entry["generate_type"] = 1
			}
			switch mode.RatioPolicy.ValueMode {
			case "smart":
				entry["ratio"] = map[string]any{"options": []string{"adaptive"}, "default": "adaptive"}
			case "", "inherit":
				entry["ratio"] = ratio
				if mode.Key == "text_to_video" && (source.Key == "MiniMax-H3" || source.Key == "MiniMax-H3-Max") {
					fixed := make([]any, 0, len(ratios))
					for _, value := range ratios {
						if value != "adaptive" {
							fixed = append(fixed, value)
						}
					}
					modeRatio := map[string]any{"options": fixed}
					if value, ok := ratio["default"].(string); ok && value != "adaptive" {
						modeRatio["default"] = value
					}
					entry["ratio"] = modeRatio
				}
			default:
				warn("创作模式 " + mode.Key + " 的比例策略无法识别，请 --refresh 重试或升级 CLI")
			}
			modes = append(modes, entry)
		}
		// Retain schema/default-mode metadata, but expose interpreted modes separately.
		var creation map[string]json.RawMessage
		if err := json.Unmarshal(fields["creation_mode_config"], &creation); err != nil {
			return nil, err
		}
		delete(creation, "modes")
		delete(out, "creation_mode_config")
		if len(creation) > 0 {
			out["creation_mode_config"] = creation
		}
		out["creation_modes"] = modes
	}
	if source.Kind != "image" && (source.Key == "MiniMax-H3" || source.Key == "MiniMax-H3-Max") && slices.Contains(ratios, "adaptive") {
		out["notes"] = []string{"MiniMax 使用 adaptive 需要参考图片或视频；纯文生视频请选择固定比例。创作模式的比例策略优先于模型级选项。"}
	}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return json.Marshal(out)
}

// Image generation accepts wire enums; video generation accepts ratio strings.
func cliRatioValue(value int64, kind string) (any, bool) {
	label := common.RatioValue(value)
	if label == "" {
		return nil, false
	}
	if kind == "image" {
		return value, true
	}
	return label, true
}

func describeDimension(d *dimensionConfig, kind string, warn func(string)) map[string]any {
	out := map[string]any{}
	if d.Label != "" {
		out["label"] = d.Label
	}
	if d.Description != "" {
		out["description"] = d.Description
	}
	if d.Required != nil {
		out["required_field"] = *d.Required
	}
	if len(d.ActiveWhenAny) > 0 {
		out["active_when_any"] = d.ActiveWhenAny
	}
	convert := func(value string) (any, bool) {
		value = strings.TrimSpace(value)
		if d.Key == "duration" {
			number, err := strconv.ParseInt(value, 10, 32)
			return number, err == nil && number > 0
		}
		if d.Key == "ratio" {
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, false
			}
			return cliRatioValue(number, kind)
		}
		if kind == "image" && d.Key == "resolution" {
			return strings.ToUpper(value), value != ""
		}
		return strings.ToLower(value), value != ""
	}
	if d.Key == "duration" {
		out["unit"] = "seconds"
	}
	validDefault := func(any) bool { return false }
	if bounds := d.RangeConfig; bounds != nil {
		if d.Key != "duration" || len(d.OptionList) != 0 || bounds.Min == nil || bounds.Max == nil || *bounds.Min <= 0 || *bounds.Max < *bounds.Min {
			warn(d.Key + " 范围配置不合法，请 --refresh 重试")
			return out
		}
		step := bounds.Step
		if step < 1 {
			step = 1
		}
		out["min"], out["max"], out["step"] = *bounds.Min, *bounds.Max, step
		validDefault = func(value any) bool {
			n := value.(int64)
			return n >= *bounds.Min && n <= *bounds.Max && (n-*bounds.Min)%step == 0
		}
	} else {
		options := []any{}
		for _, option := range d.OptionList {
			if option.Disabled {
				continue
			}
			value, ok := convert(option.Value)
			if !ok {
				warn(d.Key + " 包含无法转换的选项，已排除；请 --refresh 重试")
				continue
			}
			if !slices.Contains(options, value) {
				options = append(options, value)
			}
		}
		out["options"] = options
		if len(options) == 0 {
			warn(d.Key + " 没有可用选项，请 --refresh 重试")
		}
		validDefault = func(value any) bool { return slices.Contains(options, value) }
	}
	if d.DefaultValue != nil {
		if value, ok := convert(*d.DefaultValue); ok && validDefault(value) {
			out["default"] = value
		} else {
			warn(d.Key + " 默认值不在可用范围内，未输出默认值；请 --refresh 重试")
		}
	}
	return out
}
