package generate_image

import (
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

func TestValidateOptionsAllowsServerDecidedModel(t *testing.T) {
	opts := &Options{
		Prompt: "x",
		Model:  "seedream_3.0",
	}

	if err := ValidateOptions(opts); err != nil {
		t.Fatalf("ValidateOptions() error = %v, want nil", err)
	}
}

func TestValidateOptionsAllowsServerDecidedRatio(t *testing.T) {
	opts := &Options{
		Prompt: "x",
		Model:  "seedream_4.5",
		Ratio:  "99",
	}

	if err := ValidateOptions(opts); err != nil {
		t.Fatalf("ValidateOptions() error = %v, want nil", err)
	}
}

func TestValidateOptionsRejectsNegativeGenerateImageCount(t *testing.T) {
	count := -1
	opts := &Options{
		Prompt:             "x",
		Model:              "seedream_4.5",
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

func TestParseRatioRejectsNonInteger(t *testing.T) {
	_, err := parseRatio("1:1")
	if err == nil {
		t.Fatal("parseRatio() error = nil, want integer validation")
	}
	if !strings.Contains(err.Error(), `ratio "1:1" 必须是整数枚举值`) {
		t.Fatalf("error = %q, want integer validation", err)
	}
}

func TestValidateOptionsRejectsUnsupportedImageExtension(t *testing.T) {
	opts := &Options{
		Prompt:     "x",
		Model:      "seedream_4.5",
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
		Model:              " seedream_4.5 ",
		Ratio:              "6",
		GenerateImageCount: &count,
	}

	body := buildSubmitRunBody(opts, []string{"asset_1"})
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
	if settings.ImageModel != "seedream_4.5" {
		t.Fatalf("image_model = %q, want seedream_4.5", settings.ImageModel)
	}
	if settings.Ratio == nil || *settings.Ratio != 6 {
		t.Fatalf("ratio = %#v, want 6", settings.Ratio)
	}
	if settings.GenerateImageCount == nil || *settings.GenerateImageCount != 2 {
		t.Fatalf("generate_image_count = %#v, want 2", settings.GenerateImageCount)
	}
	assetIDs, ok := body["asset_ids"].([]string)
	if !ok || len(assetIDs) != 1 || assetIDs[0] != "asset_1" {
		t.Fatalf("asset_ids = %#v, want asset_1", body["asset_ids"])
	}
}
