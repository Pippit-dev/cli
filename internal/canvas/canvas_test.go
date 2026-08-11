package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

type fakeClient struct {
	send      func(context.Context, string, any, any) error
	multipart func(context.Context, string, map[string]string, common.MultipartFile, any) error
}

func (f *fakeClient) SendRequest(ctx context.Context, path string, body any, out any) error {
	if f.send == nil {
		return fmt.Errorf("unexpected request to %s", path)
	}
	return f.send(ctx, path, body, out)
}

func (f *fakeClient) SendRequestWithHeaders(ctx context.Context, path string, body any, _ map[string]string, out any) error {
	return f.SendRequest(ctx, path, body, out)
}

func (f *fakeClient) SendMultipartRequest(
	ctx context.Context,
	path string,
	fields map[string]string,
	file common.MultipartFile,
	out any,
) error {
	if f.multipart == nil {
		return fmt.Errorf("unexpected multipart request to %s", path)
	}
	return f.multipart(ctx, path, fields, file, out)
}

func runnerWithClient(client common.Client) *common.Runner {
	return &common.Runner{Client: client}
}

func decodeInto(out any, payload string) error {
	return json.Unmarshal([]byte(payload), out)
}

func requestJSON(t *testing.T, body any) map[string]any {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return result
}

func TestCreateReturnsRecoverableOperationIDs(t *testing.T) {
	client := &fakeClient{send: func(_ context.Context, path string, body any, out any) error {
		if path != CreatePath {
			t.Fatalf("path = %q, want %q", path, CreatePath)
		}
		request := requestJSON(t, body)
		if request["surface"] != SurfaceNovel || request["request_id"] != "request-1" {
			t.Fatalf("request = %#v, want novel request", request)
		}
		if baseValue, ok := request["Base"].(map[string]any); !ok || len(baseValue) != 0 {
			t.Fatalf("Base = %#v, want empty object", request["Base"])
		}
		if _, exists := request["team_id"]; exists {
			t.Fatalf("request = %#v, must not contain team context", request)
		}
		return decodeInto(out, `{"ret":"0","log_id":"log-create","data":{"state":"creating","project_id":"100","thread_id":"thread-1","run_id":"run-1","canvas_asset_id":"200","web_url":"https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=200"}}`)
	}}

	result, err := Create(context.Background(), CreateOptions{Title: "A", RequestID: "request-1"}, runnerWithClient(client))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.RequestID != "request-1" || result.State != StateCreating || result.ProjectID != "100" || result.CanvasAssetID != "200" {
		t.Fatalf("Create() = %#v, want recoverable creating IDs", result)
	}
}

func TestCreateWaitWithInlineOverviewStillRequiresQueryableRoot(t *testing.T) {
	queryCalls := 0
	client := &fakeClient{send: func(_ context.Context, path string, _ any, out any) error {
		switch path {
		case CreatePath:
			return decodeInto(out, `{"ret":"0","data":{"state":"creating","project_id":"100","thread_id":"thread-1","run_id":"run-1","canvas_asset_id":"200","overview_pippit_asset_id":"300","web_url":"https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=200"}}`)
		case QueryPath:
			queryCalls++
			return decodeInto(out, `{"ret":"0","data":{"Assets":[]}}`)
		default:
			return fmt.Errorf("unexpected path %s", path)
		}
	}}
	result, err := Create(context.Background(), CreateOptions{RequestID: "request-1", Wait: true}, runnerWithClient(client))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if queryCalls != 1 || result.State != StateCreating || !strings.Contains(result.Warning, "root asset is not queryable yet") {
		t.Fatalf("Create() = %#v, query calls=%d, want visibility-gated ready", result, queryCalls)
	}
}

func TestCreateTransportFailureExplainsAmbiguousOutcome(t *testing.T) {
	client := &fakeClient{send: func(context.Context, string, any, any) error {
		return errors.New("timeout")
	}}
	_, err := Create(context.Background(), CreateOptions{RequestID: "request-1"}, runnerWithClient(client))
	if err == nil || !strings.Contains(err.Error(), "outcome may be ambiguous, do not retry blindly") {
		t.Fatalf("Create() error = %v, want ambiguous outcome guidance", err)
	}
}

func TestCreateWaitsForMachineArtifactWithoutV2(t *testing.T) {
	getThreadCalls := 0
	client := &fakeClient{send: func(_ context.Context, path string, body any, out any) error {
		switch path {
		case CreatePath:
			return decodeInto(out, `{"ret":"0","data":{"state":"creating","project_id":"100","thread_id":"thread-1","run_id":"run-1","canvas_asset_id":"200","web_url":"https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=200"}}`)
		case "/api/biz/v1/skill/get_thread":
			getThreadCalls++
			request := requestJSON(t, body)
			if _, exists := request["version"]; exists {
				t.Fatalf("get_thread request = %#v, must not request v2 readable text", request)
			}
			return decodeInto(out, `{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run-1","state":3,"content":[{"sub_type":"biz/x_data_novel_script_overview","data":"{\"pippit_asset_id\":\"300\",\"canvas_asset_id\":\"200\"}"}] }]}}}`)
		case QueryPath:
			return decodeInto(out, `{"ret":"0","data":{"Assets":[{"PippitAssetID":"200"}]}}`)
		default:
			return fmt.Errorf("unexpected path %s", path)
		}
	}}

	result, err := Create(context.Background(), CreateOptions{
		RequestID:    "request-1",
		Wait:         true,
		PollInterval: time.Millisecond,
		WaitTimeout:  time.Second,
	}, runnerWithClient(client))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if getThreadCalls != 1 || result.State != StateReady || result.OverviewPippitAssetID != "300" {
		t.Fatalf("Create() = %#v, calls=%d, want ready artifact", result, getThreadCalls)
	}
	if strings.Contains(result.WebURL, "canvasId=") || !strings.Contains(result.WebURL, "overviewPippitAssetId=300") {
		t.Fatalf("WebURL = %q, want canonical overview URL", result.WebURL)
	}
}

func TestCreateKeepsCreatingWhenRootAssetIsNotQueryable(t *testing.T) {
	client := &fakeClient{send: func(_ context.Context, path string, _ any, out any) error {
		switch path {
		case CreatePath:
			return decodeInto(out, `{"ret":"0","data":{"state":"creating","project_id":"100","thread_id":"thread-1","run_id":"run-1","canvas_asset_id":"200","web_url":"https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=200"}}`)
		case "/api/biz/v1/skill/get_thread":
			return decodeInto(out, `{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run-1","state":3,"content":[{"sub_type":"biz/x_data_novel_script_overview","data":"{\"pippit_asset_id\":\"300\",\"canvas_asset_id\":\"200\"}"}]}]}}}`)
		case QueryPath:
			return decodeInto(out, `{"ret":"0","log_id":"query-log","data":{"Assets":[]}}`)
		default:
			return fmt.Errorf("unexpected path %s", path)
		}
	}}
	result, err := Create(context.Background(), CreateOptions{
		RequestID: "request-1", Wait: true, PollInterval: time.Millisecond, WaitTimeout: time.Second,
	}, runnerWithClient(client))
	if err != nil {
		t.Fatalf("Create() error = %v, want accepted processing result", err)
	}
	if result.State != StateCreating || result.ProjectID != "100" || result.OverviewPippitAssetID != "300" || !strings.Contains(result.Warning, "root asset is not queryable yet") {
		t.Fatalf("Create() = %#v, want creating state with query warning", result)
	}
	if strings.Contains(result.WebURL, "canvasId=") || !strings.Contains(result.WebURL, "overviewPippitAssetId=300") {
		t.Fatalf("WebURL = %q, want preserved canonical overview URL", result.WebURL)
	}
}

func TestCreateWaitTimeoutPreservesAcceptedIDs(t *testing.T) {
	client := &fakeClient{send: func(_ context.Context, path string, _ any, out any) error {
		switch path {
		case CreatePath:
			return decodeInto(out, `{"ret":"0","data":{"state":"creating","project_id":"100","thread_id":"thread-1","run_id":"run-1","canvas_asset_id":"200","web_url":"https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=200"}}`)
		case "/api/biz/v1/skill/get_thread":
			return decodeInto(out, `{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run-1","state":1}]}}}`)
		default:
			return fmt.Errorf("unexpected path %s", path)
		}
	}}

	result, err := Create(context.Background(), CreateOptions{
		RequestID:    "request-1",
		Wait:         true,
		PollInterval: 50 * time.Millisecond,
		WaitTimeout:  time.Millisecond,
	}, runnerWithClient(client))
	if err != nil {
		t.Fatalf("Create() error = %v, want accepted result with warning", err)
	}
	if result == nil || result.ProjectID != "100" || result.ThreadID != "thread-1" || result.RunID != "run-1" || result.CanvasAssetID != "200" {
		t.Fatalf("Create() = %#v, want all accepted IDs", result)
	}
	if result.State != StateCreating || !strings.Contains(result.Warning, "was not ready") {
		t.Fatalf("Create() = %#v, want creating state with timeout warning", result)
	}
}

func TestCreateTerminalFailureReturnsAcceptedIDsAndError(t *testing.T) {
	client := &fakeClient{send: func(_ context.Context, path string, _ any, out any) error {
		if path == CreatePath {
			return decodeInto(out, `{"ret":"0","data":{"state":"creating","project_id":"100","thread_id":"thread-1","run_id":"run-1","canvas_asset_id":"200","web_url":"https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=200"}}`)
		}
		return decodeInto(out, `{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run-1","state":4}]}}}`)
	}}
	result, err := Create(context.Background(), CreateOptions{
		RequestID: "request-1", Wait: true, PollInterval: time.Millisecond, WaitTimeout: time.Second,
	}, runnerWithClient(client))
	if result == nil || result.ProjectID != "100" || result.State != "failed" {
		t.Fatalf("Create() result = %#v, want failed result with accepted IDs", result)
	}
	if err == nil || !strings.Contains(err.Error(), "accepted canvas IDs") || !strings.Contains(err.Error(), "do not create again blindly") {
		t.Fatalf("Create() error = %v, want accepted ID guidance", err)
	}
}

func TestGetRequiresEveryRequestedStringID(t *testing.T) {
	client := &fakeClient{send: func(_ context.Context, path string, body any, out any) error {
		if path != QueryPath {
			t.Fatalf("path = %q, want query", path)
		}
		return decodeInto(out, `{"ret":"0","log_id":"log-query","data":{"Assets":[{"PippitAssetID":"2"},{"PippitAssetID":"1"}]}}`)
	}}
	result, err := Get(context.Background(), GetOptions{AssetIDs: []string{"1", "2"}}, runnerWithClient(client))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	firstID, _ := assetIDFromRaw(result.Assets[0])
	if firstID != "1" || result.LogID != "log-query" {
		t.Fatalf("Get() = %#v, want request order and log ID", result)
	}

	missingClient := &fakeClient{send: func(_ context.Context, _ string, _ any, out any) error {
		return decodeInto(out, `{"ret":"0","log_id":"missing-log","data":{"Assets":[]}}`)
	}}
	_, err = Get(context.Background(), GetOptions{AssetIDs: []string{"1"}}, runnerWithClient(missingClient))
	if err == nil || !strings.Contains(err.Error(), "did not return requested assets: 1") || !strings.Contains(err.Error(), "missing-log") {
		t.Fatalf("Get() error = %v, want strict missing asset error", err)
	}
}

func TestGetRejectsNumericAssetIDInResponse(t *testing.T) {
	client := &fakeClient{send: func(_ context.Context, _ string, _ any, out any) error {
		return decodeInto(out, `{"ret":"0","data":{"Assets":[{"PippitAssetID":123}]}}`)
	}}
	_, err := Get(context.Background(), GetOptions{AssetIDs: []string{"123"}}, runnerWithClient(client))
	if err == nil || !strings.Contains(err.Error(), "must be a JSON string") {
		t.Fatalf("Get() error = %v, want string ID validation", err)
	}
}

func TestAllocateRequiresExactUniqueStringIDs(t *testing.T) {
	client := &fakeClient{send: func(_ context.Context, path string, body any, out any) error {
		if path != AllocatePath {
			t.Fatalf("path = %q, want allocate", path)
		}
		request := requestJSON(t, body)
		if request["count"] != float64(2) {
			t.Fatalf("count = %#v, want 2", request["count"])
		}
		return decodeInto(out, `{"ret":"0","data":{"ids":["10","11"]}}`)
	}}
	result, err := Allocate(context.Background(), 2, runnerWithClient(client))
	if err != nil || strings.Join(result.AssetIDs, ",") != "10,11" {
		t.Fatalf("Allocate() = (%#v, %v), want two IDs", result, err)
	}

	duplicate := &fakeClient{send: func(_ context.Context, _ string, _ any, out any) error {
		return decodeInto(out, `{"ret":"0","data":{"ids":["10","10"]}}`)
	}}
	_, err = Allocate(context.Background(), 2, runnerWithClient(duplicate))
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Fatalf("Allocate() error = %v, want duplicate rejection", err)
	}
}

func TestApplyRequiresAcknowledgementAndEveryAssetVersion(t *testing.T) {
	version := int64(0)
	request := ApplyRequest{
		BatchID:  "batch-1",
		ClientID: "client-1",
		Transactions: []PatchTransaction{{
			TransactionID: "tx-1",
			Patches:       []PatchEntry{{AssetID: "asset-1", BaseAssetVersion: &version, Op: "add", Path: "", Value: json.RawMessage(`{"nodes":[]}`)}},
		}},
	}
	client := &fakeClient{send: func(_ context.Context, path string, body any, out any) error {
		if path != ApplyPath+"?project_id=123" {
			t.Fatalf("path = %q, want project query", path)
		}
		payload := requestJSON(t, body)
		if baseValue, ok := payload["Base"].(map[string]any); !ok || len(baseValue) != 0 {
			t.Fatalf("Base = %#v, want empty object", payload["Base"])
		}
		return decodeInto(out, `{"ret":"0","log_id":"log-apply","data":{"results":[{"transaction_id":"tx-1","status":"ack","asset_versions":{"asset-1":1}}]}}`)
	}}
	result, err := Apply(context.Background(), ApplyOptions{ProjectID: "123", Request: request}, runnerWithClient(client))
	if err != nil || result.Results[0].AssetVersions["asset-1"] != 1 {
		t.Fatalf("Apply() = (%#v, %v), want ack", result, err)
	}

	rejected := &fakeClient{send: func(_ context.Context, _ string, _ any, out any) error {
		return decodeInto(out, `{"ret":"0","data":{"results":[{"transaction_id":"tx-1","status":"blocked","blocked_by":"tx-0"}]}}`)
	}}
	_, err = Apply(context.Background(), ApplyOptions{Request: request}, runnerWithClient(rejected))
	if err == nil || !strings.Contains(err.Error(), "was not acknowledged") {
		t.Fatalf("Apply() error = %v, want status rejection", err)
	}

	missingVersion := &fakeClient{send: func(_ context.Context, _ string, _ any, out any) error {
		return decodeInto(out, `{"ret":"0","data":{"results":[{"transaction_id":"tx-1","status":"ack","asset_versions":{}}]}}`)
	}}
	_, err = Apply(context.Background(), ApplyOptions{Request: request}, runnerWithClient(missingVersion))
	if err == nil || !strings.Contains(err.Error(), "omitted version") || !strings.Contains(err.Error(), "do not replay blindly") {
		t.Fatalf("Apply() error = %v, want missing version rejection", err)
	}
}

func TestApplyBetaRejectsMultipleTransactionsBeforeRequest(t *testing.T) {
	requestCalls := 0
	client := &fakeClient{send: func(context.Context, string, any, any) error {
		requestCalls++
		return nil
	}}
	request := ApplyRequest{
		BatchID: "batch-1", ClientID: "client-1",
		Transactions: []PatchTransaction{
			{TransactionID: "tx-1", Patches: []PatchEntry{{AssetID: "asset-1", Op: "add", Path: "", Value: json.RawMessage(`{}`)}}},
			{TransactionID: "tx-2", Patches: []PatchEntry{{AssetID: "asset-2", Op: "add", Path: "", Value: json.RawMessage(`{}`)}}},
		},
	}
	_, err := Apply(context.Background(), ApplyOptions{Request: request}, runnerWithClient(client))
	if err == nil || !strings.Contains(err.Error(), "exactly one transaction") {
		t.Fatalf("Apply() error = %v, want single-transaction beta guard", err)
	}
	if requestCalls != 0 {
		t.Fatalf("request calls = %d, want validation before side effects", requestCalls)
	}
}

func TestApplyTransportFailureWarnsAgainstBlindReplay(t *testing.T) {
	request := ApplyRequest{
		BatchID: "batch-1", ClientID: "client-1",
		Transactions: []PatchTransaction{{TransactionID: "tx-1", Patches: []PatchEntry{{
			AssetID: "asset-1", Op: "replace", Path: "", Value: json.RawMessage(`{}`),
		}}}},
	}
	client := &fakeClient{send: func(context.Context, string, any, any) error { return errors.New("timeout") }}
	_, err := Apply(context.Background(), ApplyOptions{Request: request}, runnerWithClient(client))
	if err == nil || !strings.Contains(err.Error(), "do not replay blindly") {
		t.Fatalf("Apply() error = %v, want replay warning", err)
	}
}

func TestUploadWaitsForQueryableAssetWithoutRequiringCover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("video"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	client := &fakeClient{
		multipart: func(_ context.Context, gotPath string, fields map[string]string, file common.MultipartFile, out any) error {
			if gotPath != UploadPath || len(fields) != 0 {
				t.Fatalf("multipart = (%q, %#v), want upload without auth fields", gotPath, fields)
			}
			if file.FieldName != "file" || file.ContentType != "video/mp4" {
				t.Fatalf("file = %#v, want video multipart", file)
			}
			return decodeInto(out, `{"ret":"0","log_id":"upload-log","data":{"asset_id":"workspace-1","pippit_asset_id":"asset-1"}}`)
		},
		send: func(_ context.Context, path string, _ any, out any) error {
			if path != QueryPath {
				t.Fatalf("path = %q, want query", path)
			}
			return decodeInto(out, `{"ret":"0","log_id":"query-log","data":{"Assets":[{"PippitAssetID":"asset-1","SourceID":"workspace-1","Video":{"DownloadUrl":"https://example.test/clip.mp4","VID":"vid-1"}}]}}`)
		},
	}
	result, err := Upload(context.Background(), UploadOptions{
		Path: path, PollInterval: time.Millisecond, WaitTimeout: time.Second,
	}, runnerWithClient(client))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.State != StateReady || result.AssetID != "workspace-1" || result.PippitAssetID != "asset-1" || result.PollAttempts != 1 {
		t.Fatalf("Upload() = %#v, want ready identifiers", result)
	}
	if result.Locator.DownloadURL == "" || result.Locator.CoverURL != "" {
		t.Fatalf("Locator = %#v, want playable locator without cover requirement", result.Locator)
	}
}

func TestUploadWaitTimeoutPreservesDurableAssetID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("video"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	client := &fakeClient{
		multipart: func(_ context.Context, _ string, _ map[string]string, _ common.MultipartFile, out any) error {
			return decodeInto(out, `{"ret":"0","log_id":"upload-log","data":{"asset_id":"workspace-1","pippit_asset_id":"asset-1"}}`)
		},
		send: func(_ context.Context, _ string, _ any, out any) error {
			return decodeInto(out, `{"ret":"0","data":{"Assets":[]}}`)
		},
	}
	result, err := Upload(context.Background(), UploadOptions{
		Path: path, PollInterval: 50 * time.Millisecond, WaitTimeout: time.Millisecond,
	}, runnerWithClient(client))
	if err != nil {
		t.Fatalf("Upload() error = %v, want accepted processing result", err)
	}
	if result.State != StateProcessing || result.PippitAssetID != "asset-1" || result.Locator.PippitAssetID != "asset-1" {
		t.Fatalf("Upload() = %#v, want durable ID after wait timeout", result)
	}
	if !strings.Contains(result.Warning, "was not queryable") {
		t.Fatalf("warning = %q, want visibility warning", result.Warning)
	}
}

func TestEnvelopeRetIsStrict(t *testing.T) {
	client := &fakeClient{send: func(_ context.Context, _ string, _ any, out any) error {
		return decodeInto(out, `{"errmsg":"missing ret","log_id":"strict-log","data":{"Assets":[]}}`)
	}}
	_, err := Get(context.Background(), GetOptions{AssetIDs: []string{"1"}}, runnerWithClient(client))
	if err == nil || !strings.Contains(err.Error(), `ret=""`) || !strings.Contains(err.Error(), "strict-log") {
		t.Fatalf("Get() error = %v, want strict ret validation", err)
	}
}
