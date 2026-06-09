package common

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

// UploadFileOptions is the stable command-facing request shape for file upload.
type UploadFileOptions struct {
	Path     string `json:"path"`
	FileName string `json:"file_name"`
}

// UploadFileResult is the JSON envelope printed by `pippit-tool-cli short-drama +upload-file`.
type UploadFileResult struct {
	AssetID string `json:"asset_id"`
}

type uploadFileResponse struct {
	Ret     string `json:"ret"`
	Errmsg  string `json:"errmsg"`
	SvrTime int64  `json:"svr_time"`
	LogID   string `json:"log_id"`
	Data    struct {
		PippitAssetID string `json:"pippit_asset_id"`
		AssetID       string `json:"asset_id"`
	} `json:"data"`
}

const uploadFileFieldName = "file"

func UploadFile(ctx context.Context, opts UploadFileOptions, runner *Runner) (*UploadFileResult, error) {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, fmt.Errorf("上传文件已取消")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("上传文件超时")
		}
		return nil, fmt.Errorf("上传文件上下文异常: %w", err)
	}
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("上传文件运行器客户端缺失")
	}

	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return nil, fmt.Errorf("上传文件路径不能为空")
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("上传文件不存在: %s", path)
		}
		if os.IsPermission(err) {
			return nil, fmt.Errorf("没有权限读取上传文件: %s", path)
		}
		return nil, fmt.Errorf("获取上传文件信息失败: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("上传路径 %q 是目录，请指定文件", path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	fileName := filepath.Base(path)
	contentType := mime.TypeByExtension(ext)

	var resp uploadFileResponse
	if err := runner.Client.SendMultipartRequest(ctx, uploadFilePath(runner), nil, MultipartFile{
		FieldName:   uploadFileFieldName,
		Path:        path,
		FileName:    fileName,
		ContentType: contentType,
	}, &resp); err != nil {
		return nil, fmt.Errorf("上传文件请求失败: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, fmt.Errorf("上传文件请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
	}

	assetID := strings.TrimSpace(resp.Data.PippitAssetID)
	if assetID == "" {
		return nil, fmt.Errorf("上传文件响应缺少 pippit_asset_id")
	}

	return &UploadFileResult{
		AssetID: assetID,
	}, nil
}

func uploadFilePath(runner *Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.UploadFile != "" {
		return runner.Config.Paths.UploadFile
	}
	return config.UploadFilePath
}
