package canvas

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

type commandFakeClient struct {
	response string
	request  map[string]any
	handler  func(string, any, any) error
}

func (f *commandFakeClient) SendRequest(_ context.Context, path string, body any, out any) error {
	payload, _ := json.Marshal(body)
	_ = json.Unmarshal(payload, &f.request)
	if f.handler != nil {
		return f.handler(path, body, out)
	}
	return json.Unmarshal([]byte(f.response), out)
}

func (f *commandFakeClient) SendRequestWithHeaders(ctx context.Context, path string, body any, _ map[string]string, out any) error {
	return f.SendRequest(ctx, path, body, out)
}

func (f *commandFakeClient) SendMultipartRequest(context.Context, string, map[string]string, common.MultipartFile, any) error {
	return nil
}

func TestCommandExposesOnlyProviderNeutralPublicVerbs(t *testing.T) {
	cmd := NewCommand(&bytes.Buffer{}, &bytes.Buffer{}, &common.Runner{Client: &commandFakeClient{}})
	got := make([]string, 0, len(cmd.Commands()))
	for _, child := range cmd.Commands() {
		got = append(got, child.Name())
	}
	if strings.Join(got, ",") != "allocate,apply,create,get,upload" {
		t.Fatalf("commands = %v, want allocate/apply/create/get/upload", got)
	}
	for _, forbidden := range []string{"import", "bind", "team"} {
		if strings.Contains(strings.ToLower(cmd.CommandPath()+" "+cmd.Short+" "+strings.Join(got, " ")), forbidden) {
			t.Fatalf("public command surface contains forbidden verb %q", forbidden)
		}
	}
}

func TestAllocateCommandPrintsOneMachineReadableJSONLine(t *testing.T) {
	client := &commandFakeClient{response: `{"ret":"0","log_id":"log-1","data":{"ids":["10","11"]}}`}
	var stdout, stderr bytes.Buffer
	cmd := NewCommand(&stdout, &stderr, &common.Runner{Client: client})
	cmd.SetArgs([]string{"allocate", "--count", "2"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("stdout = %q, want one JSON line", stdout.String())
	}
	var result map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if len(result["asset_ids"].([]any)) != 2 || client.request["count"] != float64(2) {
		t.Fatalf("result/request = (%#v, %#v), want two allocated IDs", result, client.request)
	}
}

func TestCreateCommandPrintsOneMachineReadableJSONLine(t *testing.T) {
	client := &commandFakeClient{response: `{"ret":"0","log_id":"log-1","data":{"state":"creating","project_id":"100","thread_id":"thread-1","run_id":"run-1","canvas_asset_id":"200","web_url":"https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=200"}}`}
	var stdout, stderr bytes.Buffer
	cmd := NewCommand(&stdout, &stderr, &common.Runner{Client: client})
	cmd.SetArgs([]string{"create", "--title", "Demo", "--request-id", "request-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("stdout = %q, want one JSON line", stdout.String())
	}
	var result map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if result["request_id"] != "request-1" || result["project_id"] != "100" || client.request["surface"] != "novel" {
		t.Fatalf("result/request = (%#v, %#v), want personal novel create", result, client.request)
	}
}

func TestCreateCommandWaitTimeoutPrintsAcceptedIDs(t *testing.T) {
	client := &commandFakeClient{handler: func(path string, _ any, out any) error {
		response := `{"ret":"0","data":{"thread":{"run_list":[{"run_id":"run-1","state":1}]}}}`
		if path == "/api/biz/v1/skill/canvas/create" {
			response = `{"ret":"0","data":{"state":"creating","project_id":"100","thread_id":"thread-1","run_id":"run-1","canvas_asset_id":"200","web_url":"https://xyq.jianying.com/novel/detail/canvas?projectId=100&canvasId=200"}}`
		}
		return json.Unmarshal([]byte(response), out)
	}}
	var stdout, stderr bytes.Buffer
	cmd := NewCommand(&stdout, &stderr, &common.Runner{Client: client})
	cmd.SetArgs([]string{
		"create", "--request-id", "request-1", "--wait",
		"--poll-interval", "50ms", "--timeout", "1ms",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, want accepted create outcome", err)
	}
	var result map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		t.Fatalf("stdout = %q, want JSON: %v", stdout.String(), err)
	}
	if result["project_id"] != "100" || result["state"] != "creating" || result["warning"] == "" {
		t.Fatalf("result = %#v, want accepted IDs and wait warning", result)
	}
}

func TestApplyRequestRejectsTeamOrImportExtensions(t *testing.T) {
	for _, field := range []string{"team_id", "provider", "import_source"} {
		payload := `{"batch_id":"b","client_id":"c","transactions":[],"` + field + `":"x"}`
		_, err := readApplyRequest(strings.NewReader(payload), "-")
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("readApplyRequest(%s) error = %v, want unknown field rejection", field, err)
		}
	}
}

func TestApplyTransportResultFlagIsHiddenAndPrintsExplicitReject(t *testing.T) {
	client := &commandFakeClient{response: `{"ret":"0","log_id":"transport-log","data":{"results":[{"transaction_id":"tx-1","status":"reject","message":"version conflict","current_asset_versions":{"asset-1":9},"server_detail":{"reason":"conflict"}}]}}`}
	var stdout, stderr bytes.Buffer
	cmd := NewCommand(&stdout, &stderr, &common.Runner{Client: client})
	var applyCommand *cobra.Command
	for _, child := range cmd.Commands() {
		if child.Name() == "apply" {
			applyCommand = child
			break
		}
	}
	if applyCommand == nil {
		t.Fatal("apply command not found")
	}
	flag := applyCommand.Flags().Lookup("transport-result")
	if flag == nil || !flag.Hidden {
		t.Fatalf("transport-result flag = %#v, want hidden flag", flag)
	}

	request := `{"batch_id":"batch-1","client_id":"client-1","transactions":[{"transaction_id":"tx-1","patches":[{"asset_id":"asset-1","op":"replace","path":"","value":{}}]}]}`
	cmd.SetIn(strings.NewReader(request))
	cmd.SetArgs([]string{"apply", "--file", "-", "--transport-result"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var output map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &output); err != nil {
		t.Fatalf("stdout = %q, want JSON: %v", stdout.String(), err)
	}
	result := output["results"].([]any)[0].(map[string]any)
	if result["status"] != "reject" || result["message"] != "version conflict" || result["server_detail"] == nil {
		t.Fatalf("result = %#v, want complete explicit reject", result)
	}
}

func TestApplyTransportResultPrintsRootCreateAckWithoutVersion(t *testing.T) {
	client := &commandFakeClient{response: `{"ret":"0","data":{"results":[{"transaction_id":"tx-1","status":"ack","server_detail":{"created":true}}]}}`}
	var stdout, stderr bytes.Buffer
	cmd := NewCommand(&stdout, &stderr, &common.Runner{Client: client})
	request := `{"batch_id":"batch-1","client_id":"client-1","transactions":[{"transaction_id":"tx-1","patches":[{"asset_id":"asset-new","op":"add","path":"","value":{}}]}]}`
	cmd.SetIn(strings.NewReader(request))
	cmd.SetArgs([]string{"apply", "--file", "-", "--transport-result"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var output map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &output); err != nil {
		t.Fatalf("stdout = %q, want JSON: %v", stdout.String(), err)
	}
	result := output["results"].([]any)[0].(map[string]any)
	if result["status"] != "ack" || result["asset_versions"] != nil || result["server_detail"] == nil {
		t.Fatalf("result = %#v, want unchanged root-create ACK", result)
	}
}
