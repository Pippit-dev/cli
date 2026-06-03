package common

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type accessKeyAuthorizer struct {
	accessKey string
}

func NewAccessKeyAuthorizer(accessKey string) RequestAuthorizer {
	return &accessKeyAuthorizer{accessKey: strings.TrimSpace(accessKey)}
}

func (a *accessKeyAuthorizer) Inject(ctx context.Context, req *http.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.accessKey == "" && req.Method == http.MethodPost {
		return fmt.Errorf("%s 缺失. 请前往小云雀官网个人设置页创建 Access Key，地址：https://xyq.jianying.com/home?tab_name=home\n配置后重试：\n  export %s=\"<your-access-key>\"", config.EnvXYQAccessKey, config.EnvXYQAccessKey)
	}
	if a.accessKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.accessKey)
	}
	return nil
}
