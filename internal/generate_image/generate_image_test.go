package generate_image

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateOptionsRequiresModel(t *testing.T) {
	opts := &Options{
		Prompt: "x",
	}

	err := ValidateOptions(opts)
	if err == nil {
		t.Fatal("ValidateOptions() error = nil, want model validation")
	}
	if !strings.Contains(err.Error(), "缺少必填参数 --model") {
		t.Fatalf("error = %q, want model validation", err)
	}
}

func TestValidateOptionsAllowsDynamicModelName(t *testing.T) {
	opts := &Options{
		Prompt: "x",
		Model:  "图片测试模型",
	}

	if err := ValidateOptions(opts); err != nil {
		t.Fatalf("ValidateOptions() error = %v, want nil", err)
	}
}

func TestValidateOptionsAllowsServerDecidedRatio(t *testing.T) {
	opts := &Options{
		Prompt: "x",
		Model:  "图片测试模型",
		Ratio:  "99",
	}

	if err := ValidateOptions(opts); err != nil {
		t.Fatalf("ValidateOptions() error = %v, want nil", err)
	}
}

func TestValidateOptionsAllowsServerDecidedResolution(t *testing.T) {
	opts := &Options{
		Prompt:     "x",
		Model:      "图片测试模型",
		Resolution: "8K",
	}

	if err := ValidateOptions(opts); err != nil {
		t.Fatalf("ValidateOptions() error = %v, want nil", err)
	}
}

func TestValidateOptionsRejectsNegativeGenerateImageCount(t *testing.T) {
	count := -1
	opts := &Options{
		Prompt:             "x",
		Model:              "图片测试模型",
		GenerateImageCount: &count,
	}

	err := ValidateOptions(opts)
	if err == nil {
		t.Fatal("ValidateOptions() error = nil, want generate-image-count validation")
	}
	if !strings.Contains(err.Error(), "--generate-image-count 不能为负数") {
		t.Fatalf("error = %q, want generate-image-count validation", err)
	}
}

func TestParseRatioSupportsVisibleEnumValues(t *testing.T) {
	cases := []struct {
		ratio string
		want  int
	}{
		{ratio: "0", want: 0},
		{ratio: "2", want: 2},
		{ratio: "13", want: 13},
		{ratio: "3", want: 3},
		{ratio: "4", want: 4},
		{ratio: "5", want: 5},
		{ratio: "6", want: 6},
		{ratio: "7", want: 7},
		{ratio: "8", want: 8},
		{ratio: "9", want: 9},
		{ratio: "10", want: 10},
		{ratio: "11", want: 11},
		{ratio: "12", want: 12},
		{ratio: "99", want: 99},
	}

	for _, tt := range cases {
		t.Run(tt.ratio, func(t *testing.T) {
			got, err := parseRatio(tt.ratio)
			if err != nil {
				t.Fatalf("parseRatio(%q) error = %v", tt.ratio, err)
			}
			if got == nil || *got != tt.want {
				t.Fatalf("parseRatio(%q) = %#v, want %d", tt.ratio, got, tt.want)
			}
		})
	}
}

func TestImageOptionalParametersAndEffortPassThrough(t *testing.T) {
	for _, effort := range []string{"", " HIGH ", "future-effort"} {
		opts := &Options{Prompt: "image", Model: "未来图片模型", Effort: effort}
		if err := ValidateOptions(opts); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(buildSubmitRunBody(opts, "future-image-model", nil))
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Settings map[string]json.RawMessage `json:"general_agent_settings"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"ratio", "resolution", "generate_image_count"} {
			if _, exists := body.Settings[key]; exists {
				t.Fatalf("unspecified %s was filled: %s", key, raw)
			}
		}
		if effort == "" {
			if _, exists := body.Settings["image_effort"]; exists {
				t.Fatalf("unspecified effort was filled: %s", raw)
			}
		} else {
			var got string
			if err := json.Unmarshal(body.Settings["image_effort"], &got); err != nil || got != strings.ToLower(strings.TrimSpace(effort)) {
				t.Fatalf("effort not forwarded: %s, %v", raw, err)
			}
		}
	}
}

func TestParseRatioRejectsNonInteger(t *testing.T) {
	for _, ratio := range []string{"9:16", "adaptive", "17:11", "3.0"} {
		t.Run(ratio, func(t *testing.T) {
			err := ValidateOptions(&Options{Prompt: "image", Model: "未来图片模型", Ratio: ratio})
			if err == nil || !strings.Contains(err.Error(), "必须是整数枚举值") {
				t.Fatalf("ratio %q: error = %v, want integer enum validation", ratio, err)
			}
		})
	}
}

func TestValidateOptionsRejectsUnsupportedImageExtension(t *testing.T) {
	opts := &Options{
		Prompt:     "x",
		Model:      "图片测试模型",
		ImagePaths: []string{"ref.tiff"},
	}

	err := ValidateOptions(opts)
	if err == nil {
		t.Fatal("ValidateOptions() error = nil, want image extension validation")
	}
	if !strings.Contains(err.Error(), `不支持的图片文件后缀 ".tiff"`) {
		t.Fatalf("error = %q, want image extension validation", err)
	}
}

func TestBuildSubmitRunBodyWithGeneralAgentSettings(t *testing.T) {
	count := 2
	opts := &Options{
		Prompt:             "  生成小猫海报  ",
		Model:              " Seedream 5.0 Pro ",
		Ratio:              "6",
		Resolution:         " 4k ",
		GenerateImageCount: &count,
	}

	body := buildSubmitRunBody(opts, "seedream_5.0_pro", []string{"asset_1"})
	if body["agent_name"] != agentNameNest {
		t.Fatalf("agent_name = %v, want nest agent", body["agent_name"])
	}
	if body["message"] != "生成小猫海报" {
		t.Fatalf("message = %v, want trimmed prompt", body["message"])
	}
	settings, ok := body["general_agent_settings"].(generalAgentSettings)
	if !ok {
		t.Fatalf("general_agent_settings = %#v, want object", body["general_agent_settings"])
	}
	if settings.ImageModel != "seedream_5.0_pro" {
		t.Fatalf("image_model = %q, want seedream_5.0_pro", settings.ImageModel)
	}
	if settings.Ratio == nil || *settings.Ratio != 6 {
		t.Fatalf("ratio = %#v, want 6", settings.Ratio)
	}
	if settings.Resolution != "4K" {
		t.Fatalf("resolution = %q, want 4K", settings.Resolution)
	}
	if settings.GenerateImageCount == nil || *settings.GenerateImageCount != 2 {
		t.Fatalf("generate_image_count = %#v, want 2", settings.GenerateImageCount)
	}
	assetIDs, ok := body["asset_ids"].([]string)
	if !ok || len(assetIDs) != 1 || assetIDs[0] != "asset_1" {
		t.Fatalf("asset_ids = %#v, want asset_1", body["asset_ids"])
	}
}
