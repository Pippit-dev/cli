package generate_image

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
)

const (
	agentNameNest = "pippit_nest_agent"
)

var (
	allowedImageExtensionList = []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg"}
	allowedImageExtensions    = common.StringSet(allowedImageExtensionList)
)

const ratioUsage = "enum values: 0=原始比例/自动, 2=16:9(横屏), 13=21:9(电影), 3=9:16(竖屏), 4=4:3, 5=3:4, 6=1:1"

// Options is the stable command-facing request shape for generate-image.
type Options struct {
	Prompt             string
	ImagePaths         []string
	Model              string
	Ratio              string
	Resolution         string
	GenerateImageCount *int
}

type generalAgentSettings struct {
	ImageModel         string `json:"image_model"`
	Ratio              *int   `json:"ratio,omitempty"`
	Resolution         string `json:"resolution,omitempty"`
	GenerateImageCount *int   `json:"generate_image_count,omitempty"`
}

// Result is the JSON envelope printed by `pippit-tool-cli generate-image`.
type Result struct {
	ThreadID      string `json:"thread_id"`
	RunID         string `json:"run_id"`
	WebThreadLink string `json:"web_thread_link"`
}

func Run(ctx context.Context, opts *Options, runner *common.Runner) (*Result, error) {
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("generate-image 运行器客户端缺失")
	}
	if err := ValidateOptions(opts); err != nil {
		return nil, err
	}

	imageAssetIDs, err := uploadImageList(ctx, opts.ImagePaths, runner)
	if err != nil {
		return nil, fmt.Errorf("上传图片失败: %w", err)
	}

	body := buildSubmitRunBody(opts, imageAssetIDs)

	var resp common.SubmitRunResponse
	if err := runner.Client.SendRequest(ctx, common.SubmitRunPath(runner), body, &resp); err != nil {
		return nil, fmt.Errorf("提交 generate-image 请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, common.NewLogIDError(fmt.Sprintf("generate-image 请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg), resp.LogID)
	}
	if resp.Data.Run.ThreadID == "" {
		return nil, fmt.Errorf("generate-image 响应缺少 data.run.thread_id")
	}
	if resp.Data.Run.RunID == "" {
		return nil, fmt.Errorf("generate-image 响应缺少 data.run.run_id")
	}

	return &Result{
		ThreadID:      resp.Data.Run.ThreadID,
		RunID:         resp.Data.Run.RunID,
		WebThreadLink: resp.Data.WebThreadLink,
	}, nil
}

func ValidateOptions(opts *Options) error {
	if opts == nil {
		return fmt.Errorf("缺少必填参数 --prompt")
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return fmt.Errorf("缺少必填参数 --prompt")
	}
	if strings.TrimSpace(opts.Model) == "" {
		return fmt.Errorf("缺少必填参数 --model")
	}
	if _, err := parseRatio(opts.Ratio); err != nil {
		return err
	}
	if opts.GenerateImageCount != nil && *opts.GenerateImageCount < 0 {
		return fmt.Errorf("--generate-image-count 不能为负数")
	}
	if err := validateImageExtensions(opts.ImagePaths); err != nil {
		return err
	}
	return nil
}

func validateImageExtensions(paths []string) error {
	for _, path := range paths {
		ext := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
		if _, ok := allowedImageExtensions[ext]; !ok {
			return fmt.Errorf("不支持的图片文件后缀 %q，文件：%q；支持的后缀：%s", ext, path, strings.Join(allowedImageExtensionList, ", "))
		}
	}
	return nil
}

func SupportedRatioUsage() string {
	return ratioUsage
}

func uploadImageList(ctx context.Context, paths []string, runner *common.Runner) ([]string, error) {
	assetIDs := make([]string, 0, len(paths))
	for _, path := range paths {
		expanded, err := common.ExpandPath(path)
		if err != nil {
			return nil, err
		}
		result, err := common.UploadFile(ctx, common.UploadFileOptions{Path: expanded}, runner)
		if err != nil {
			return nil, err
		}
		assetIDs = append(assetIDs, result.AssetID)
	}
	return assetIDs, nil
}

func buildSubmitRunBody(opts *Options, imageAssetIDs []string) map[string]any {
	ratio, _ := parseRatio(opts.Ratio)
	body := map[string]any{
		"agent_name": agentNameNest,
		"message":    strings.TrimSpace(opts.Prompt),
		"general_agent_settings": generalAgentSettings{
			ImageModel:         strings.TrimSpace(opts.Model),
			Ratio:              ratio,
			Resolution:         strings.ToUpper(strings.TrimSpace(opts.Resolution)),
			GenerateImageCount: opts.GenerateImageCount,
		},
	}
	if len(imageAssetIDs) > 0 {
		body["asset_ids"] = imageAssetIDs
	}
	return body
}

func parseRatio(raw string) (*int, error) {
	ratio := strings.TrimSpace(raw)
	if ratio == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(ratio)
	if err != nil {
		return nil, fmt.Errorf("ratio %q 必须是整数枚举值；可参考：%s", ratio, ratioUsage)
	}
	return &value, nil
}
