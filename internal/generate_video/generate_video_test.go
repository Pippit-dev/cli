package generate_video

import (
	"encoding/json"
	"testing"
)

func TestBuildSubmitRunBodyPreservesEmptyPrompt(t *testing.T) {
	body := buildSubmitRunBody(&Options{}, nil, nil, nil)
	got, err := json.Marshal(body["video_part_tool_param"])
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(got) != `{"prompt":""}` {
		t.Fatalf("video_part_tool_param = %s, want explicit empty prompt", got)
	}
}

func TestMiniMaxModelParametersPassThrough(t *testing.T) {
	for _, model := range []string{"MiniMax-H3", "MiniMax-H3-Max"} {
		t.Run(model, func(t *testing.T) {
			opts := &Options{Prompt: "cat walking", Model: model, Ratio: "16:9"}
			if err := ValidateOptions(opts); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(buildSubmitRunBody(opts, nil, nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			var body struct {
				AgentName string         `json:"agent_name"`
				Param     map[string]any `json:"video_part_tool_param"`
			}
			if err := json.Unmarshal(data, &body); err != nil {
				t.Fatal(err)
			}
			if body.AgentName != "pippit_video_part_agent" || body.Param["model"] != model || body.Param["ratio"] != "16:9" {
				t.Fatalf("unexpected body: %s", data)
			}
			for _, field := range []string{"duration_sec", "resolution", "generate_type"} {
				if _, exists := body.Param[field]; exists {
					t.Fatalf("CLI must leave %s defaults to the API: %s", field, data)
				}
			}
			// API owns the Max 21:9 rejection; the CLI must not add an enum gate.
			opts.Ratio = "21:9"
			if err := ValidateOptions(opts); err != nil {
				t.Fatal(err)
			}
		})
	}
}
