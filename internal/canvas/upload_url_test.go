package canvas

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestUploadURLPreservesRecoveryAndErrors(t *testing.T) {
	const uploaded = `{"ret":"0","log_id":"upload-log","data":{"asset_id":"source-id","pippit_asset_id":"asset-url"}}`
	for _, tc := range []struct {
		name        string
		upload      string
		query       string
		timeout     bool
		wantErr     string
		wantWarn    string
		wantQueries int
	}{
		{"query_timeout", uploaded, "", true, "", "not queryable", 1},
		{"query_failure", uploaded, `{"ret":"1","errmsg":"denied","log_id":"query-log"}`, false, "", "query-log", 1},
		{"media_not_ready", uploaded, `{"ret":"0","data":{"Assets":[{"PippitAssetID":"asset-url","AssetType":1,"Image":{}}]}}`, false, "", "cloud media ID is not ready", 1},
		{"rejected", `{"ret":"1","errmsg":"rejected","log_id":"upload-log"}`, "", false, "upload-log", "", 0},
		{"missing_id", `{"ret":"0","log_id":"upload-log","data":{"asset_id":"source-id"}}`, "", false, "upload-log", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uploads, queries := 0, 0
			client := &fakeClient{send: func(ctx context.Context, path string, body, out any) error {
				switch path {
				case UploadPath:
					uploads++
					return decodeInto(out, tc.upload)
				case QueryPath:
					queries++
					if tc.timeout {
						<-ctx.Done()
						return ctx.Err()
					}
					return decodeInto(out, tc.query)
				default:
					t.Fatalf("unexpected request: %s", path)
					return nil
				}
			}}
			waitTimeout := time.Second
			if tc.timeout {
				waitTimeout = time.Millisecond
			}
			result, err := Upload(context.Background(), UploadOptions{
				URL: "https://assets.example.invalid/media", WaitTimeout: waitTimeout,
			}, runnerWithClient(client))
			if uploads != 1 || queries != tc.wantQueries {
				t.Fatalf("uploads=%d queries=%d; import must not be replayed", uploads, queries)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || result != nil {
					t.Fatalf("result=%+v err=%v, want upload error with LogID", result, err)
				}
				return
			}
			if err != nil || result == nil {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result.State != StateProcessing || result.PippitAssetID != "asset-url" || result.AssetID != "source-id" ||
				result.Locator.PippitAssetID != "asset-url" || result.MediaAssetID != "" ||
				!strings.Contains(result.Warning, tc.wantWarn) {
				t.Fatalf("result=%+v, want recoverable processing result", result)
			}
		})
	}
}
