package common

import (
	"context"
	"encoding/json"
	"errors"
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

func TestGetThreadPreservesStructuredErrorDataWithoutLoggingIt(t *testing.T) {
	_, err := GetThread(context.Background(), &GetThreadOptions{ThreadID: "thread_123", RunID: "run_456", PreserveErrorData: true}, &Runner{
		Client: getThreadFakeClient{response: `{"ret":"5","errmsg":"生成失败","log_id":"log_123","data":{"thread":{"thread_id":"thread_123","run_list":[{"run_id":"run_456","state":4}]},"private_marker":"not-for-error-output"}}`},
	})
	var logErr *LogIDError
	if !errors.As(err, &logErr) || logErr.LogID() != "log_123" || !strings.Contains(string(logErr.RawData), `"state":4`) {
		t.Fatalf("structured error data or LogID lost: %#v", err)
	}
	encoded, marshalErr := json.Marshal(logErr)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(err.Error(), "not-for-error-output") || strings.Contains(string(encoded), "not-for-error-output") || strings.Contains(string(encoded), "RawData") {
		t.Fatalf("structured error data leaked into error output")
	}
}

func TestGetThreadOmitsStructuredErrorDataByDefault(t *testing.T) {
	_, err := GetThread(context.Background(), &GetThreadOptions{ThreadID: "thread_123", RunID: "run_456"}, &Runner{
		Client: getThreadFakeClient{response: `{"ret":"5","errmsg":"legacy failure","log_id":"log_123","data":{"private_marker":"not-retained"}}`},
	})
	var logErr *LogIDError
	if !errors.As(err, &logErr) || len(logErr.RawData) != 0 || err.Error() != "获取线程请求返回失败: ret=5 errmsg=legacy failure log_id=log_123" {
		t.Fatalf("default GetThread error contract changed: %v", err)
	}
}
