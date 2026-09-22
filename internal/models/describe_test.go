package models

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func description(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()
	out, err := describeModel(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(out, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestDescriptionCLIParametersAndRawPreservation(t *testing.T) {
	raw := `{"key":"MiniMax-H3","kind":"video","is_default":true,"supported_ratio_list":[0,2,13,3,4,5,6],"default_ratio":3,"config_key":"internal",
	"future_field":9007199254740993,"audio_total_limit":0,"max_image_size":31457280,"min_video_duration":2000,
	"supported_duration_list":[{"value":999}],"default_duration_value":999,
	"parameter_config":{"dimensions":[
	{"key":"duration","label":"视频时长","description":"生成的视频时长","required_field":true,"default_value":"10","range_config":{"min_value":4,"max_value":15,"step":1}},
	{"key":"resolution","label":"视频分辨率","description":"输出清晰度","required_field":false,"default_value":" 768P ","option_list":[{"value":"768p"},{"value":"2k"},{"value":"4k","disabled":true}]},
	{"key":"seed","default_value":"random"}],"need_available_combinations":true}}
	`
	out := description(t, raw)
	for _, key := range []string{"config_key", "is_default", "supported_ratio_list", "default_ratio", "supported_duration_list", "default_duration_value", "audio_total_limit", "max_image_size"} {
		if _, ok := out[key]; ok {
			t.Fatalf("unconverted field %s", key)
		}
	}
	var ratio struct {
		Options []string
		Default string
	}
	if err := json.Unmarshal(out["ratio"], &ratio); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ratio.Options, []string{"adaptive", "16:9", "21:9", "9:16", "4:3", "3:4", "1:1"}) || ratio.Default != "9:16" {
		t.Fatalf("ratio=%+v", ratio)
	}
	var resolution struct {
		Options            []string
		Default            string
		Label, Description string
		Required           *bool `json:"required_field"`
	}
	if err := json.Unmarshal(out["resolution"], &resolution); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolution.Options, []string{"768p", "2k"}) || resolution.Default != "768p" {
		t.Fatalf("resolution=%+v", resolution)
	}
	if resolution.Label != "视频分辨率" || resolution.Description != "输出清晰度" || resolution.Required == nil || *resolution.Required {
		t.Fatalf("video resolution metadata lost: %+v", resolution)
	}
	var duration struct {
		Min, Max, Step, Default int
		Unit                    string
		Label, Description      string
		Required                *bool `json:"required_field"`
	}
	if err := json.Unmarshal(out["duration"], &duration); err != nil {
		t.Fatal(err)
	}
	if duration.Min != 4 || duration.Max != 15 || duration.Step != 1 || duration.Default != 10 || duration.Unit != "seconds" {
		t.Fatalf("duration=%+v", duration)
	}
	if duration.Label != "视频时长" || duration.Description != "生成的视频时长" || duration.Required == nil || !*duration.Required {
		t.Fatalf("video duration metadata lost: %+v", duration)
	}
	if string(out["future_field"]) != "9007199254740993" {
		t.Fatal("integer precision lost")
	}
	var limits map[string]json.RawMessage
	if err := json.Unmarshal(out["material_limits"], &limits); err != nil {
		t.Fatal(err)
	}
	if string(limits["audio_total_limit"]) != "0" || string(limits["max_image_size_bytes"]) != "31457280" || string(limits["min_video_duration_ms"]) != "2000" {
		t.Fatalf("limits=%s", out["material_limits"])
	}
	if _, ok := limits["video_total_limit"]; ok {
		t.Fatal("absent limit must stay absent")
	}
	if !bytes.Contains(out["parameter_config"], []byte("seed")) || !bytes.Contains(out["parameter_config"], []byte("need_available_combinations")) {
		t.Fatal("unrelated configuration lost")
	}
	if _, ok := out["warnings"]; ok {
		t.Fatalf("valid dimensions must override legacy durations: %s", out["warnings"])
	}
	if !bytes.Contains(out["notes"], []byte("纯文生视频")) {
		t.Fatal("missing MiniMax adaptive restriction")
	}
}

func TestDescriptionUnknownRatioAndDefault(t *testing.T) {
	for _, value := range []int{1, 999} {
		raw, _ := json.Marshal(map[string]any{"supported_ratio_list": []int{value, 2, 2}, "default_ratio": value})
		out := description(t, string(raw))
		var ratio struct {
			Options []string
			Default *string
		}
		if err := json.Unmarshal(out["ratio"], &ratio); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ratio.Options, []string{"16:9"}) || ratio.Default != nil {
			t.Fatalf("invented usable value: %+v", ratio)
		}
		if _, exists := out["warnings"]; exists {
			t.Fatal("unknown and custom enums should be silently skipped")
		}
	}
	out := description(t, `{"supported_ratio_list":[0,7,8,9,10,11,12],"default_ratio":0}`)
	var ratio struct {
		Options []string
		Default string
	}
	if err := json.Unmarshal(out["ratio"], &ratio); err != nil {
		t.Fatal(err)
	}
	if ratio.Default != "adaptive" || !reflect.DeepEqual(ratio.Options, []string{"adaptive", "2:1", "2.35:1", "1.85:1", "1.125:2.436", "3:2", "2:3"}) {
		t.Fatalf("ratio=%+v", ratio)
	}
}

func TestDescriptionDimensionFailuresAndOptions(t *testing.T) {
	for _, dimension := range []string{
		`{"key":"resolution","default_value":"2k","option_list":[{"value":"768p"},{"value":"2k","disabled":true}]}`,
		`{"key":"resolution","default_value":"","option_list":[]}`,
		`{"key":"duration","default_value":"7","range_config":{"min_value":4,"max_value":10,"step":2}}`,
		`{"key":"duration","range_config":{"min_value":10,"max_value":4}}`,
		`{"key":"duration","option_list":[{"value":"auto"}]}`,
	} {
		out := description(t, `{"parameter_config":{"dimensions":[`+dimension+`]}}`)
		if _, ok := out["warnings"]; !ok {
			t.Fatalf("missing warning: %s", dimension)
		}
		for _, key := range []string{"resolution", "duration"} {
			var value map[string]json.RawMessage
			if len(out[key]) == 0 {
				continue
			}
			if err := json.Unmarshal(out[key], &value); err != nil {
				t.Fatal(err)
			}
			if _, ok := value["default"]; ok {
				t.Fatalf("invalid default exposed: %s", out[key])
			}
		}
	}
	out := description(t, `{"parameter_config":{"dimensions":[{"key":"duration","default_value":"10","option_list":[{"value":"5"},{"value":"10"},{"value":"15","disabled":true}]}]}}`)
	var duration struct {
		Options []int
		Default int
		Min     *int
	}
	if err := json.Unmarshal(out["duration"], &duration); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(duration.Options, []int{5, 10}) || duration.Default != 10 || duration.Min != nil {
		t.Fatalf("duration=%+v", duration)
	}
}

func TestDescriptionCreationModesAndCacheNotMutated(t *testing.T) {
	raw := json.RawMessage(`{"key":"MiniMax-H3-Max","kind":"video","supported_ratio_list":[0,2],"default_ratio":2,"creation_mode_config":{"schema_version":1,"default_mode":"text_to_video","modes":[{"key":"text_to_video","enabled":true,"ratio_policy":{"value_mode":"inherit"}},{"key":"first_last_frame","enabled":true,"ratio_policy":{"value_mode":"smart"},"validation_policy":{"required_video_count":0,"forbidden_input_types":["video","audio"]}}]}}`)
	before := append([]byte(nil), raw...)
	var catalog Catalog
	if err := json.Unmarshal([]byte(`{"config":{"models":[]}}`), &catalog); err != nil {
		t.Fatal(err)
	}
	catalog.Config.Models = append(catalog.Config.Models, raw)
	result, err := catalog.Describe("MiniMax-H3-Max")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Modes []struct {
			GenerateType int `json:"generate_type"`
			Ratio        struct {
				Options []string
				Default string
			}
			Validation struct {
				Required  *int     `json:"required_video_count"`
				Forbidden []string `json:"forbidden_input_types"`
			} `json:"validation_policy"`
		} `json:"creation_modes"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Modes) != 2 || out.Modes[0].Ratio.Default != "16:9" || !reflect.DeepEqual(out.Modes[0].Ratio.Options, []string{"16:9"}) || out.Modes[1].GenerateType != 1 || out.Modes[1].Ratio.Default != "adaptive" || !reflect.DeepEqual(out.Modes[1].Ratio.Options, []string{"adaptive"}) {
		t.Fatalf("modes=%+v", out.Modes)
	}
	if out.Modes[1].Validation.Required == nil || *out.Modes[1].Validation.Required != 0 || !reflect.DeepEqual(out.Modes[1].Validation.Forbidden, []string{"video", "audio"}) {
		t.Fatal("mode requirements lost")
	}
	if !bytes.Equal(before, catalog.Config.Models[0]) {
		t.Fatal("description mutated cached raw config")
	}
}

func TestImageDescriptionParametersAndConstraints(t *testing.T) {
	raw := `{"key":"future-image","kind":"image","is_default":true,
	"supported_ratio_list":[0,2,6,13,1,999],"default_ratio":6,
	"parameter_config":{"dimensions":[
	{"key":"resolution","label":"图片分辨率","required_field":false,"default_value":"2k","option_list":[{"value":"2k","label":"高清"},{"value":"4K","disabled":true}]},
	{"key":"effort","label":"推理强度","description":"计算档位","required_field":true,"default_value":"HIGH","active_when_any":[{"resolution":["2k"]}],"option_list":[{"value":"low"},{"value":"HIGH","label":"高","description":"更多计算"},{"value":"max","disabled":true}]},
	{"key":"future-dimension","option_list":[{"value":"future-choice"}]}],
	"need_available_combinations":true,"combination_dimension_keys":["resolution","effort"],
	"default_combination":{"resolution":"2k","effort":"HIGH"},
	"available_combinations":[{"option_values":{"resolution":"2k","effort":"HIGH"}}]},
	"creation_mode_config":{"modes":[{"key":"reference_generation","enabled":true}]}}
	`
	out := description(t, raw)
	var ratio struct {
		Options []int64
		Default int64
		Labels  map[string]string `json:"option_labels"`
	}
	var resolution, effort struct {
		Options     []string
		Default     string
		Required    *bool           `json:"required_field"`
		Active      json.RawMessage `json:"active_when_any"`
		Label       string
		Description string
	}
	for key, target := range map[string]any{"ratio": &ratio, "resolution": &resolution, "effort": &effort} {
		if err := json.Unmarshal(out[key], target); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(ratio.Options, []int64{0, 2, 6, 13}) || ratio.Default != 6 || !reflect.DeepEqual(ratio.Labels, map[string]string{"0": "adaptive", "2": "16:9", "6": "1:1", "13": "21:9"}) {
		t.Fatalf("ratio=%+v", ratio)
	}
	if !reflect.DeepEqual(resolution.Options, []string{"2K"}) || resolution.Default != "2K" || resolution.Required == nil || *resolution.Required {
		t.Fatalf("resolution=%+v", resolution)
	}
	if !reflect.DeepEqual(effort.Options, []string{"low", "high"}) || effort.Default != "high" || effort.Required == nil || !*effort.Required || effort.Label != "推理强度" || effort.Description != "计算档位" || string(effort.Active) != `[{"resolution":["2k"]}]` {
		t.Fatalf("effort=%+v", effort)
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &source); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, source["parameter_config"]); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out["parameter_config"], compact.Bytes()) {
		t.Fatalf("raw dimension metadata or combination rules changed: %s", out["parameter_config"])
	}
	if bytes.Contains(out["creation_mode_config"], []byte("generate_type")) || len(out["creation_modes"]) != 0 {
		t.Fatal("image modes must not acquire video generation selectors")
	}
	if _, exists := out["is_default"]; exists {
		t.Fatal("model default marker exposed")
	}
}

func TestImageDescriptionOptionalEffortAndRatioDimension(t *testing.T) {
	out := description(t, `{"kind":"image","supported_ratio_list":[2],"parameter_config":{"dimensions":[{"key":"ratio","default_value":"13","option_list":[{"value":"13"},{"value":"6"},{"value":"2","disabled":true},{"value":"999"},{"value":"1"}]}]}}`)
	if len(out["effort"]) != 0 || len(out["resolution"]) != 0 {
		t.Fatal("absent capabilities must not be invented")
	}
	var ratio struct {
		Options []int64
		Default int64
		Labels  map[string]string `json:"option_labels"`
	}
	if err := json.Unmarshal(out["ratio"], &ratio); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ratio.Options, []int64{13, 6}) || ratio.Default != 13 || !reflect.DeepEqual(ratio.Labels, map[string]string{"13": "21:9", "6": "1:1"}) {
		t.Fatalf("ratio dimension did not override legacy list: %+v", ratio)
	}
	for _, raw := range []string{
		`{"kind":"image","parameter_config":{"dimensions":[{"key":"effort","default_value":"high","option_list":[{"value":"low"},{"value":"high","disabled":true}]}]}}`,
		`{"kind":"image","parameter_config":{"dimensions":[{"key":"effort","option_list":[]}]}}`,
	} {
		out := description(t, raw)
		if len(out["warnings"]) == 0 || bytes.Contains(out["effort"], []byte(`"default"`)) {
			t.Fatalf("invalid effort configuration accepted: %s", out["effort"])
		}
	}
	_, err := describeModel(json.RawMessage(`{"kind":"image","parameter_config":{"dimensions":[{"key":"effort"},{"key":"effort"}]}}`))
	if err == nil {
		t.Fatal("duplicate effort dimension accepted")
	}
}

func TestImageRatioZeroDefaultAndUnavailableValues(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"image","supported_ratio_list":[0,0,3,1,999],"default_ratio":0}`,
		`{"kind":"image","parameter_config":{"dimensions":[{"key":"ratio","default_value":"0","option_list":[{"value":"0"},{"value":"0"},{"value":"3"},{"value":"6","disabled":true},{"value":"1"},{"value":"999"}]}]}}`,
	} {
		out := description(t, raw)
		var ratio struct {
			Options []int64
			Default *int64
			Labels  map[string]string `json:"option_labels"`
		}
		if err := json.Unmarshal(out["ratio"], &ratio); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ratio.Options, []int64{0, 3}) || ratio.Default == nil || *ratio.Default != 0 || !reflect.DeepEqual(ratio.Labels, map[string]string{"0": "adaptive", "3": "9:16"}) {
			t.Fatalf("numeric zero default or available ratio values changed: %s", out["ratio"])
		}
	}
}
