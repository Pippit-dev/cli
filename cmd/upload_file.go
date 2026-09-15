package cmd

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/common"
	"github.com/spf13/cobra"
)

const maxMediaUploadBytes int64 = 500 * 1000 * 1000

func newUploadFileCommand(stdout, stderr io.Writer, runner *common.Runner) *cobra.Command {
	var opts common.UploadFileOptions
	cmd := &cobra.Command{
		Use:   "upload-file",
		Short: "Upload an image, video, or MP3/WAV audio file",
		Args:  cobra.NoArgs,
		RunE: withErrorLog("upload-file", nil, func(cmd *cobra.Command, _ []string) error {
			opts.Path = strings.TrimSpace(opts.Path)
			if err := validateMediaUpload(opts.Path); err != nil {
				return err
			}
			result, err := common.UploadFile(cmd.Context(), opts, runner)
			if err != nil {
				return err
			}
			return common.WriteJSON(stdout, result)
		}),
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.Flags().StringVar(&opts.Path, "path", "", "local media file to upload (required, less than 500 MB)")
	return cmd
}

func validateMediaUpload(path string) error {
	if path == "" {
		return fmt.Errorf("缺少必填参数 --path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("获取上传文件信息失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("上传路径必须是普通文件")
	}
	if info.Size() >= maxMediaUploadBytes {
		return fmt.Errorf("上传文件必须小于 500 MB（500000000 字节）")
	}
	ext := strings.ToLower(filepath.Ext(path))
	contentType := mime.TypeByExtension(ext)
	if strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "video/") {
		return nil
	}
	if ext == ".mp3" || ext == ".wav" {
		return nil
	}
	return fmt.Errorf("不支持的文件类型 %q，仅支持图片、视频和 .mp3/.wav 音频", ext)
}
