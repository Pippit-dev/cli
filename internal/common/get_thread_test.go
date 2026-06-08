package common

import (
	"context"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
)

type getThreadFakeClient struct {
	response string
}

func (c getThreadFakeClient) SendRequest(_ context.Context, _ string, _ any, out any) error {
	return sonic.Unmarshal([]byte(c.response), out)
}

func (c getThreadFakeClient) SendRequestWithHeaders(ctx context.Context, path string, body any, _ map[string]string, out any) error {
	return c.SendRequest(ctx, path, body, out)
}

func (c getThreadFakeClient) SendMultipartRequest(context.Context, string, map[string]string, MultipartFile, any) error {
	return nil
}

func TestGetThreadAllowsStructuredDataWithoutVersion(t *testing.T) {
	result, err := GetThread(context.Background(), &GetThreadOptions{
		ThreadID: "thread_123",
		RunID:    "run_456",
	}, &Runner{
		Client: getThreadFakeClient{response: `{"ret":"0","errmsg":"","data":{"thread":{"thread_id":"thread_123","run_list":[{"run_id":"run_456","state":3}]}}}`},
	})
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
	}
	if result.ReadableText != "" {
		t.Fatalf("ReadableText = %q, want empty for non-v2 response", result.ReadableText)
	}
	if !strings.Contains(string(result.RawData), `"thread"`) {
		t.Fatalf("RawData = %s, want structured thread data", string(result.RawData))
	}
}

func TestGetThreadRequiresRetZero(t *testing.T) {
	_, err := GetThread(context.Background(), &GetThreadOptions{
		ThreadID: "thread_123",
		RunID:    "run_456",
	}, &Runner{
		Client: getThreadFakeClient{response: `{"errmsg":"","data":{"thread":{"thread_id":"thread_123"}}}`},
	})
	if err == nil {
		t.Fatal("GetThread() error = nil, want missing ret validation")
	}
	if !strings.Contains(err.Error(), "获取线程请求返回失败: ret=") {
		t.Fatalf("error = %q, want missing ret validation", err)
	}
}

func TestGetThreadV2RequiresReadableText(t *testing.T) {
	_, err := GetThread(context.Background(), &GetThreadOptions{
		ThreadID: "thread_123",
		RunID:    "run_456",
		Version:  GetThreadVersionV2,
	}, &Runner{
		Client: getThreadFakeClient{response: `{"ret":"0","errmsg":"","data":{"thread":{"thread_id":"thread_123"}}}`},
	})
	if err == nil {
		t.Fatal("GetThread() error = nil, want v2 readable_text validation")
	}
	if !strings.Contains(err.Error(), "get_thread v2 响应缺少 data.readable_text") {
		t.Fatalf("error = %q, want readable_text validation", err)
	}
}
