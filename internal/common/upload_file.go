package common

import (
	"context"
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

var allowedUploadExtensions = map[string]bool{
	".doc": true,
	".txt": true,
}

func UploadFile(ctx context.Context, opts UploadFileOptions, runner *Runner) (*UploadFileResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runner == nil || runner.Client == nil {
		return nil, fmt.Errorf("upload_file runner client is required")
	}

	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return nil, fmt.Errorf("upload file path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat upload file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("upload path %q is a directory", path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	if !allowedUploadExtensions[ext] {
		return nil, fmt.Errorf("unsupported file extension %q; only .doc and .txt uploads are supported", ext)
	}
	fileName := filepath.Base(path)
	contentType := mime.TypeByExtension(ext)

	var resp uploadFileResponse
	if err := runner.Client.SendMultipartRequest(ctx, uploadFilePath(runner), nil, MultipartFile{
		FieldName:   uploadFileFieldName,
		Path:        path,
		FileName:    fileName,
		ContentType: contentType,
	}, &resp); err != nil {
		return nil, fmt.Errorf("upload_file request failed: %w", err)
	}
	if resp.Ret != "0" {
		if resp.Errmsg == "" {
			resp.Errmsg = "unknown error"
		}
		return nil, fmt.Errorf("upload_file failed: ret=%s errmsg=%s", resp.Ret, resp.Errmsg)
	}

	assetID := strings.TrimSpace(resp.Data.PippitAssetID)
	if assetID == "" {
		assetID = strings.TrimSpace(resp.Data.AssetID)
	}
	if assetID == "" {
		return nil, fmt.Errorf("upload_file response missing pippit_asset_id")
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
