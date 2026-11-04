package generate_video

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

func TestQueryFailureReasons(t *testing.T) {
	for _, tt := range []struct{ name, run, want string }{
		{"real fail reason", `{"fail_reason":{"message":"视频可能不合规，请修改后重试","code":12023}}`, "视频可能不合规，请修改后重试 (error_code=12023)"},
		{"message only", `{"fail_reason":{"message":"reason"}}`, "reason"},
		{"zero code", `{"fail_reason":{"message":"reason","code":0}}`, "reason"},
		{"code only", `{"fail_reason":{"code":12023}}`, "error_code=12023"},
		{"empty reason", `{"fail_reason":{"message":" ","code":0}}`, ""},
		{"null reason", `{"fail_reason":null}`, ""},
		{"structured reason wins", `{"fail_reason":{"message":"real reason","code":12023},"error_message":"legacy"}`, "real reason (error_code=12023)"},
		{"legacy message fallback", `{"fail_reason":{"code":12023},"error_message":"legacy"}`, "legacy"},
		{"error_msg fallback", `{"error_msg":"legacy"}`, "legacy"},
		{"errmsg fallback", `{"errmsg":"legacy"}`, "legacy"},
		{"artifact reason", `{"entry_list":[{"artifact":{"content":[{"data":{"error_message":"artifact reason","error_code":99}}]}}]}`, "artifact reason (error_code=99)"},
		{"artifact code only", `{"entry_list":[{"artifact":{"content":[{"data":{"error_code":"99"}}]}}]}`, "error_code=99"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var run queryRun
			if err := json.Unmarshal([]byte(tt.run), &run); err != nil {
				t.Fatal(err)
			}
			if got := extractQueryErrorMessage(run); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

type queryErrorClient struct{ err error }

func (c queryErrorClient) SendRequest(context.Context, string, any, any) error { return c.err }
func (c queryErrorClient) SendRequestWithHeaders(context.Context, string, any, map[string]string, any) error {
	return c.err
}
func (c queryErrorClient) SendMultipartRequest(context.Context, string, map[string]string, common.MultipartFile, any) error {
	return c.err
}

func TestQueryResultTransportFailure(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled, errors.New("connection reset")} {
		t.Run(cause.Error(), func(t *testing.T) {
			opts := &QueryResultOptions{ThreadID: "thread", RunID: "run", DownloadDir: t.TempDir()}
			result, err := QueryResult(context.Background(), opts, &common.Runner{Client: queryErrorClient{cause}})
			if result != nil || !errors.Is(err, cause) {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			result = NewQueryResultError(err, opts)
			if result.Status != "unknown" || result.Completed || result.ErrorMessage == "" {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}
