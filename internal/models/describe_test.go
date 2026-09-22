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
	raw := `{"key":"MiniMax-H3","is_default":true,"supported_ratio_list":[0,2,13,3,4,5,6],"default_ratio":3,"config_key":"internal",
	"future_field":9007199254740993,"audio_total_limit":0,"max_image_size":31457280,"min_video_duration":2000,
	"supported_duration_list":[{"value":999}],"default_duration_value":999,
	"parameter_config":{"dimensions":[
	{"key":"duration","default_value":"10","range_config":{"min_value":4,"max_value":15,"step":1}},
	{"key":"resolution","default_value":" 768P ","option_list":[{"value":"768p"},{"value":"2k"},{"value":"4k","disabled":true}]},
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
		Options []string
		Default string
	}
	if err := json.Unmarshal(out["resolution"], &resolution); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolution.Options, []string{"768p", "2k"}) || resolution.Default != "768p" {
		t.Fatalf("resolution=%+v", resolution)
	}
	var duration struct {
		Min, Max, Step, Default int
		Unit                    string
	}
	if err := json.Unmarshal(out["duration"], &duration); err != nil {
		t.Fatal(err)
	}
	if duration.Min != 4 || duration.Max != 15 || duration.Step != 1 || duration.Default != 10 || duration.Unit != "seconds" {
		t.Fatalf("duration=%+v", duration)
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
