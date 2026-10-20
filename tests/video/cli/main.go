// 本地分支的 PPE 测试入口；业务命令、鉴权和请求体复用正式 CLI。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Pippit-dev/pippit-cli/cmd"
	"github.com/Pippit-dev/pippit-cli/internal/adapter"
	"github.com/Pippit-dev/pippit-cli/internal/config"
	marketingtest "github.com/Pippit-dev/pippit-cli/tests/marketing"
)

type ppeAdapter struct {
	adapter.Default
	env    string
	output string
}

func (a ppeAdapter) PrepareAPIRequest(ctx context.Context, req *http.Request) (adapter.AuthDecision, error) {
	if err := marketingtest.PrepareROIRequest(req, a.output); err != nil {
		return adapter.UseDefaultAuth, err
	}
	return a.Default.PrepareAPIRequest(ctx, req)
}

func (a ppeAdapter) WrapTransport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return captureTransport{next: next, ppeAdapter: a}
}

type captureTransport struct {
	ppeAdapter
	next http.RoundTripper
}

func writeNewJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

func (t captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Path != config.SubmitRunPath {
		return t.next.RoundTrip(req)
	}
	if req.Method != http.MethodPost || req.GetBody == nil || req.Header.Get("x-tt-env") != t.env || req.Header.Get("x-use-ppe") != "1" {
		return nil, fmt.Errorf("测试提交缺少 POST body 或 PPE 路由")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	err = json.NewDecoder(body).Decode(&payload)
	body.Close()
	if err != nil {
		return nil, err
	}
	switch payload["agent_name"] {
	case "pippit_video_part_agent":
		if payload["call_source"] != "pippit_cli" {
			return nil, fmt.Errorf("当前 Video Part 请求未携带 call_source=pippit_cli，拒绝提交")
		}
	case "pippit_nest_agent":
		settings, _ := payload["general_agent_settings"].(map[string]any)
		model, _ := settings["image_model"].(string)
		if strings.TrimSpace(model) == "" {
			return nil, fmt.Errorf("当前 Nest 请求未指定图片模型，拒绝提交")
		}
	default:
		return nil, fmt.Errorf("测试入口仅支持 Video Part 或指定模型的 Nest 生图请求")
	}
	// O_EXCL 在发送前记录尝试；同一证据目录拒绝重交或重定向重放。
	if err := writeNewJSON(filepath.Join(t.output, "request.json"), map[string]any{
		"time": time.Now().UTC().Format(time.RFC3339), "env": t.env,
		"method": req.Method, "path": req.URL.Path, "body": payload,
		"babi_param": req.URL.Query().Get("babi_param"),
		"headers":    map[string]string{"x-tt-env": t.env, "x-use-ppe": "1", "x-team-id": req.Header.Get("x-team-id")},
	}); err != nil {
		return nil, fmt.Errorf("记录请求失败；不得重交本次测试: %w", err)
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var result struct {
		Ret   string `json:"ret"`
		LogID string `json:"log_id"`
		Data  struct {
			Run struct {
				ThreadID string `json:"thread_id"`
				RunID    string `json:"run_id"`
			} `json:"run"`
		} `json:"data"`
	}
	decodeErr := json.Unmarshal(raw, &result)
	if result.LogID == "" {
		result.LogID = resp.Header.Get("x-tt-logid")
	}
	metadata := map[string]any{
		"http_status": resp.StatusCode, "ret": result.Ret, "log_id": result.LogID,
		"thread_id": result.Data.Run.ThreadID, "run_id": result.Data.Run.RunID,
		"json_decoded": decodeErr == nil,
	}
	if err := writeNewJSON(filepath.Join(t.output, "response.json"), metadata); err != nil {
		return nil, fmt.Errorf("请求已发送但响应证据保存失败，不得重交: %w", err)
	}
	return resp, nil
}

func main() {
	cfg, err := config.LoadFile(config.FileName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	env, output := cfg.PPEEnv, os.Getenv("VIDEO_TEST_OUTPUT")
	info, err := os.Stat(output)
	if !strings.HasPrefix(env, "ppe_") || env == "ppe_test" || err != nil || !info.IsDir() {
		fmt.Fprintln(os.Stderr, "请通过 tests/video/check_billing.py 指定专属 PPE 泳道和证据目录")
		os.Exit(1)
	}
	if err := cmd.ExecuteWithAdapter(ppeAdapter{env: env, output: output}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
