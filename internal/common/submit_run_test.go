package common

import (
	"context"
	"strings"
	"testing"
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
