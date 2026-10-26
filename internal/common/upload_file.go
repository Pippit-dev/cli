package common

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

// UploadFileOptions is the stable command-facing request shape for file upload.
type UploadFileOptions struct {
	Path     string `json:"path"`
	URL      string `json:"url,omitempty"`
	FileName string `json:"file_name"`
}

// ValidateSource checks the source before either command-level file validation
// or direct callers can read a file or send a request.
func (opts UploadFileOptions) ValidateSource() error {
	path, sourceURL := strings.TrimSpace(opts.Path), strings.TrimSpace(opts.URL)
	if (path == "") == (sourceURL == "") {
		return fmt.Errorf("--path 与 --url 必须且只能提供一个")
	}
	if sourceURL != "" {
		u, err := url.Parse(sourceURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return fmt.Errorf("--url 必须是有效的 HTTPS 素材链接，不能包含用户凭据或 fragment")
		}
	}
	return nil
}

// UploadFileResult is the shared JSON output for media and short-drama uploads.
type UploadFileResult struct {
	TeamID  string `json:"team_id"`
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

var uploadFileContentTypeFallbacks = map[string]string{
	".mp3": "audio/mpeg",
	".wav": "audio/wav",
}

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

	if err := opts.ValidateSource(); err != nil {
		return nil, err
	}

	var resp uploadFileResponse
	if sourceURL := strings.TrimSpace(opts.URL); sourceURL != "" {
		if err := runner.Client.SendRequest(ctx, uploadFilePath(runner), map[string]string{"url": sourceURL}, &resp); err != nil {
			return nil, fmt.Errorf("URL 导入请求失败，结果可能未确认，请检查资产后再重试: %w", err)
		}
	} else {
		path := strings.TrimSpace(opts.Path)
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
		if contentType == "" {
			contentType = uploadFileContentTypeFallbacks[ext]
		}
		if err := runner.Client.SendMultipartRequest(ctx, uploadFilePath(runner), nil, MultipartFile{
			FieldName:   uploadFileFieldName,
			Path:        path,
			FileName:    fileName,
			ContentType: contentType,
		}, &resp); err != nil {
			return nil, fmt.Errorf("上传文件请求失败: %w", err)
		}
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "未知错误"
		}
		return nil, NewLogIDError(fmt.Sprintf("上传文件请求返回失败: ret=%s errmsg=%s", resp.Ret, resp.Errmsg), resp.LogID)
	}

	assetID := strings.TrimSpace(resp.Data.PippitAssetID)
	if assetID == "" {
		return nil, NewLogIDError("上传文件响应缺少 pippit_asset_id", resp.LogID)
	}

	return &UploadFileResult{
		TeamID:  SpaceTeamID(ctx),
		AssetID: assetID,
	}, nil
}

func uploadFilePath(runner *Runner) string {
	if runner != nil && runner.Config != nil && runner.Config.Paths != nil && runner.Config.Paths.UploadFile != "" {
		return runner.Config.Paths.UploadFile
	}
	return config.UploadFilePath
}
