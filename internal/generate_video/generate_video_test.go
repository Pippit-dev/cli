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
