package generate_audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

// DefaultModel is retained only for calls using legacy convenience flags.
const DefaultModel = "seedaudio_1.0"

// Options keeps the audio parameter object independent of the request envelope.
type Options struct {
	Parameters      map[string]json.RawMessage
	LocalReferences []LocalReference
}

type LocalReference struct {
	Type string
	Path string
}

func Run(ctx context.Context, opts *Options, runner *common.Runner) (*common.SubmitRunResult, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("generate-audio 运行器客户端缺失")
	}
	if opts == nil {
		return nil, fmt.Errorf("generate-audio 参数缺失")
	}
	params := make(map[string]json.RawMessage, len(opts.Parameters)+1)
	for key, value := range opts.Parameters {
		params[key] = value
	}
	var refs []json.RawMessage
	if len(opts.LocalReferences) > 0 {
		if raw, exists := params["references"]; exists {
			if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) || json.Unmarshal(raw, &refs) != nil {
				return nil, fmt.Errorf("追加本地参考素材时，references 必须是 JSON 数组")
			}
		}
	}
	// Check every local file before the first upload. UploadFile still checks each
	// file when used, since a file can change after this preflight.
	paths := make([]string, len(opts.LocalReferences))
	for i, ref := range opts.LocalReferences {
		path, err := readableReferencePath(ref.Path)
		if err != nil {
			return nil, fmt.Errorf("--%s: %w", ref.Type, err)
		}
		paths[i] = path
	}
	for i, ref := range opts.LocalReferences {
		upload, err := common.UploadFile(ctx, common.UploadFileOptions{Path: paths[i]}, runner)
		if err != nil {
			return nil, fmt.Errorf("上传音频生成参考素材失败: %w", err)
		}
		raw, err := json.Marshal(map[string]string{"type": ref.Type, "pippit_asset_id": upload.AssetID})
		if err != nil {
			return nil, err
		}
		refs = append(refs, raw)
	}
	if len(opts.LocalReferences) > 0 {
		raw, err := json.Marshal(refs)
		if err != nil {
			return nil, err
		}
		params["references"] = raw
	}
	return common.SubmitRun(ctx, "generate-audio", map[string]any{
		"agent_name":            "pippit_audio_part_agent",
		"message":               requestMessage(params),
		"audio_part_tool_param": params,
	}, runner)
}

func requestMessage(params map[string]json.RawMessage) string {
	for _, key := range []string{"prompt", "text", "task_type"} {
		var value string
		if json.Unmarshal(params[key], &value) == nil && strings.TrimSpace(value) != "" {
			if key == "task_type" {
				return "音频任务：" + value
			}
			return value
		}
	}
	return "音频生成任务"
}

func readableReferencePath(value string) (string, error) {
	path, err := common.ExpandPath(value)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("读取参考文件失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("参考路径 %q 不是普通文件", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开参考文件失败: %w", err)
	}
	defer file.Close()
	var sample [1]byte
	if _, err := file.Read(sample[:]); err != nil && err != io.EOF {
		return "", fmt.Errorf("读取参考文件失败: %w", err)
	}
	return path, nil
}
