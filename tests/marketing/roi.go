// Package marketing provides test-only request preparation for the local CLI entrypoint.
package marketing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// PrepareROIRequest mirrors SubmitRunWithBabiParam's query encoding without
// changing the production marketing command, request body, identity or PPE headers.
func PrepareROIRequest(req *http.Request, evidenceDir string) error {
	if req.URL.Path != "/api/biz/v1/agent/submit_marketing_run" {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(evidenceDir, "roi.json"))
	if os.IsNotExist(err) {
		return nil // 保留不补 ROI 的对照用例。
	}
	if err != nil {
		return err
	}
	var param map[string]string
	if err := json.Unmarshal(raw, &param); err != nil {
		return fmt.Errorf("ROI fixture 必须为字符串对象: %w", err)
	}
	for key := range param {
		switch key {
		case "scene_lv1", "scene_lv2", "tool_id", "tab_name", "edit_type", "enter_from":
		default:
			return fmt.Errorf("ROI fixture 不支持字段 %s，仅允许业务标签", key)
		}
	}
	for _, key := range []string{"scene_lv1", "scene_lv2", "tool_id"} {
		if strings.TrimSpace(param[key]) == "" {
			return fmt.Errorf("ROI fixture 缺少 %s", key)
		}
	}
	query, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return fmt.Errorf("解析营销 query 失败: %w", err)
	}
	encoded, err := json.Marshal(param)
	if err != nil {
		return err
	}
	if !query.Has("babi_param") {
		query.Set("babi_param", string(encoded))
	}
	req.URL.RawQuery = query.Encode()
	// 在发送前记录实际 query；O_EXCL 防止同一用例重复发送。
	file, err := os.OpenFile(filepath.Join(evidenceDir, "roi-request.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("记录 ROI 请求失败，拒绝重交: %w", err)
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(map[string]string{
		"path": req.URL.Path, "babi_param": query.Get("babi_param"),
	})
}
