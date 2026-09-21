package generate_audio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

// Match the existing Canvas JSON input limit; this is a local I/O bound,
// independent of model parameter validation.
const maxInputBytes = 64 << 20

func readParameters(input, filePath string, stdin io.Reader) (map[string]json.RawMessage, error) {
	var reader io.Reader = strings.NewReader(input)
	if filePath != "" {
		if filePath == "-" {
			reader = stdin
		} else {
			path, err := common.ExpandPath(filePath)
			if err != nil {
				return nil, err
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil, fmt.Errorf("读取 --file 失败: %w", err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("--file 必须是普通文件或 -")
			}
			file, err := os.Open(path)
			if err != nil {
				return nil, fmt.Errorf("打开 --file 失败: %w", err)
			}
			defer file.Close()
			reader = file
		}
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取音频参数失败: %w", err)
	}
	if len(payload) > maxInputBytes {
		return nil, fmt.Errorf("音频参数超过 64 MiB 限制")
	}
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || payload[0] != '{' || !json.Valid(payload) {
		return nil, fmt.Errorf("音频参数必须是单个合法 JSON 对象，不能包含尾随内容")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := checkJSONKeys(decoder); err != nil {
		return nil, err
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(payload, &params); err != nil {
		return nil, err
	}
	for key := range params {
		normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
		switch normalized {
		case "agentname", "message", "audioparttoolparam", "videoparttoolparam", "generalagentsettings", "assetids", "threadid", "runid", "authorization", "accesskey", "ak", "token", "teamid", "uid", "userid", "headers", "baseurl":
			return nil, fmt.Errorf("音频参数不能包含请求外层或身份字段 %q；--input/--file 只接受 audio_part_tool_param 对象", key)
		}
	}
	return params, nil
}

// Reject duplicate keys instead of silently keeping the last value. UseNumber
// avoids converting arbitrary JSON numbers to float64 during this check.
func checkJSONKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := make(map[string]bool)
	for decoder.More() {
		if delim == '{' {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key := token.(string)
			if seen[key] {
				return fmt.Errorf("JSON 对象包含重复字段 %q", key)
			}
			seen[key] = true
		}
		if err := checkJSONKeys(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func addFlag(params map[string]json.RawMessage, key, flag string, value any) error {
	if _, exists := params[key]; exists {
		return fmt.Errorf("--%s 与 JSON 字段 %s 冲突，请只保留一个来源", flag, key)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("--%s 必须可编码为 JSON，数值必须为有限数值: %w", flag, err)
	}
	params[key] = raw
	return nil
}
