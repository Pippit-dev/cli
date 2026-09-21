package generate_video

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/Pippit-dev/pippit-cli/internal/config"
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

type videoSubmitRecordingClient struct {
	common.Client
	path string
	body any
}

func (c *videoSubmitRecordingClient) SendRequest(_ context.Context, path string, body, out any) error {
	c.path, c.body = path, body
	return json.Unmarshal([]byte(`{"ret":"0","data":{"run":{"thread_id":"thread_123","run_id":"run_123"}}}`), out)
}

func TestRunCarriesVideoROIQuery(t *testing.T) {
	for _, model := range []string{
		"MiniMax-H3", "MiniMax-H3-Max", "wan3.0", "happyhorse-1.1",
		"Seedance_2.5", "seedance2.0_vision", "seedance2.0_fast_vision",
		"Seedance_2.0_mini", "Seedance_2.0_mini_lite", "", "future-model",
	} {
		t.Run(model, func(t *testing.T) {
			client := &videoSubmitRecordingClient{}
			runner := &common.Runner{
				Client: client,
				Config: &config.Config{Paths: &config.Paths{SubmitRun: "/custom/submit_run?source=a%2Bb&item=1&item=2"}},
			}
			if _, err := Run(context.Background(), &Options{Prompt: "cat walking", Model: model}, runner); err != nil {
				t.Fatal(err)
			}
			path, err := url.Parse(client.path)
			if err != nil {
				t.Fatal(err)
			}
			query := path.Query()
			if path.Path != "/custom/submit_run" || query.Get("source") != "a+b" || len(query["item"]) != 2 {
				t.Fatalf("configured path/query lost: %s", client.path)
			}
			var babi map[string]string
			if err := json.Unmarshal([]byte(query.Get("babi_param")), &babi); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]string{
				"scene_lv1": "ai_agent", "scene_lv2": "front_tool", "tool_id": "instant_video",
				"tab_name": "other", "edit_type": "instant_video", "enter_from": "skill",
			} {
				if babi[key] != want {
					t.Fatalf("babi_param[%s] = %q, want %q", key, babi[key], want)
				}
			}
			body := client.body.(map[string]any)
			if body["agent_name"] != common.AgentNameVideoPart || body["message"] != "cat walking" || len(body) != 3 {
				t.Fatalf("ROI must not change the request body contract: %#v", body)
			}
		})
	}
}

func TestRunPreservesExplicitROIQuery(t *testing.T) {
	for _, model := range []string{"MiniMax-H3", "Seedance_2.0_mini"} {
		t.Run(model, func(t *testing.T) {
			client := &videoSubmitRecordingClient{}
			raw := `{"scene_lv1":"ai_agent","scene_lv2":"front_tool","tool_id":"custom_video"}`
			runner := &common.Runner{Client: client, Config: &config.Config{Paths: &config.Paths{
				SubmitRun: "/custom/submit_run?babi_param=" + url.QueryEscape(raw),
			}}}
			if _, err := Run(context.Background(), &Options{Prompt: "cat walking", Model: model}, runner); err != nil {
				t.Fatal(err)
			}
			path, err := url.Parse(client.path)
			if err != nil || path.Query().Get("babi_param") != raw {
				t.Fatalf("explicit attribution overwritten: %s, %v", client.path, err)
			}
		})
	}
}

func TestRunRejectsMalformedQueryBeforeSubmit(t *testing.T) {
	client := &videoSubmitRecordingClient{}
	runner := &common.Runner{Client: client, Config: &config.Config{Paths: &config.Paths{
		SubmitRun: "/custom/submit_run?source=%invalid",
	}}}
	if _, err := Run(context.Background(), &Options{Prompt: "cat walking", Model: "MiniMax-H3"}, runner); err == nil {
		t.Fatal("expected malformed URL query error")
	}
	if client.path != "" {
		t.Fatal("malformed URL must not submit a request")
	}
}

func TestConfiguredVideoModelParametersPassThrough(t *testing.T) {
	for _, model := range []string{"MiniMax-H3", "MiniMax-H3-Max", "wan3.0", "happyhorse-1.1"} {
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
