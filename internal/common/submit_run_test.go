package common

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

func TestSubmitRunReturnsSharedResult(t *testing.T) {
	result, err := SubmitRun(context.Background(), "generate-video", map[string]any{"message": "test"}, &Runner{
		Client: getThreadFakeClient{response: `{"ret":"0","errmsg":"","data":{"run":{"thread_id":"thread_123","run_id":"run_456"},"web_thread_link":"https://example.com/thread_123"}}`},
	})
	if err != nil {
		t.Fatalf("SubmitRun() error = %v", err)
	}
	if result.ThreadID != "thread_123" || result.RunID != "run_456" || result.WebThreadLink != "https://example.com/thread_123" {
		t.Fatalf("SubmitRun() result = %#v", result)
	}
}

func TestSubmitRunBusinessErrorIncludesLogID(t *testing.T) {
	_, err := SubmitRun(context.Background(), "video-super-resolution", nil, &Runner{
		Client: getThreadFakeClient{response: `{"ret":"16008","errmsg":"提交Run任务失败","log_id":"log_123"}`},
	})
	if err == nil || !strings.Contains(err.Error(), "video-super-resolution 请求返回失败") || !strings.Contains(err.Error(), "log_id=log_123") {
		t.Fatalf("SubmitRun() error = %v, want command and log_id", err)
	}
}

type submitRunHeaderFakeClient struct {
	response string
	path     string
	headers  map[string]string
}

func (c *submitRunHeaderFakeClient) SendRequest(ctx context.Context, path string, body any, out any) error {
	return c.SendRequestWithHeaders(ctx, path, body, nil, out)
}

func (c *submitRunHeaderFakeClient) SendRequestWithHeaders(_ context.Context, path string, _ any, headers map[string]string, out any) error {
	c.path = path
	c.headers = headers
	return json.Unmarshal([]byte(c.response), out)
}

func (c *submitRunHeaderFakeClient) SendMultipartRequest(context.Context, string, map[string]string, MultipartFile, any) error {
	return nil
}

func TestSubmitRunAddsPPEHeadersFromEnv(t *testing.T) {
	t.Setenv(config.EnvPPECliEnv, " ppe_novel_agent_opt ")
	client := &submitRunHeaderFakeClient{response: `{"ret":"0","data":{"run":{"thread_id":"thread_123","run_id":"run_456"}}}`}
	_, err := SubmitRun(context.Background(), "submit-run", map[string]any{"message": "test"}, &Runner{Client: client})
	if err != nil {
		t.Fatalf("SubmitRun() error = %v", err)
	}
	if client.headers["x-use-ppe"] != "1" || client.headers["x-tt-env"] != "ppe_novel_agent_opt" {
		t.Fatalf("PPE headers = %#v", client.headers)
	}
}

func TestSubmitRunRejectsInvalidPPEEnv(t *testing.T) {
	t.Setenv(config.EnvPPECliEnv, "prod")
	client := &submitRunHeaderFakeClient{response: `{"ret":"0","data":{"run":{"thread_id":"thread_123","run_id":"run_456"}}}`}
	_, err := SubmitRun(context.Background(), "submit-run", map[string]any{"message": "test"}, &Runner{Client: client})
	if err == nil || !strings.Contains(err.Error(), config.EnvPPECliEnv) {
		t.Fatalf("SubmitRun() error = %v, want PPE env validation", err)
	}
	if client.path != "" {
		t.Fatalf("invalid PPE env must not submit request, path=%q", client.path)
	}
}
