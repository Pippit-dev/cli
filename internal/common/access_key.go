package common

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type accessKeyAuthorizer struct {
	accessKey func() string
}

func NewAccessKeyAuthorizer(accessKey string) RequestAuthorizer {
	trimmed := strings.TrimSpace(accessKey)
	return NewAccessKeyProviderAuthorizer(func() string { return trimmed })
}

// NewAccessKeyProviderAuthorizer resolves the Access Key immediately before
// each request. This lets interactive commands update their in-memory runtime
// configuration without rebuilding the shared HTTP client.
func NewAccessKeyProviderAuthorizer(accessKey func() string) RequestAuthorizer {
	if accessKey == nil {
		accessKey = func() string { return "" }
	}
	return &accessKeyAuthorizer{accessKey: accessKey}
}

func (a *accessKeyAuthorizer) Inject(ctx context.Context, req *http.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	accessKey := ""
	if a != nil && a.accessKey != nil {
		accessKey = strings.TrimSpace(a.accessKey())
	}
	if accessKey == "" && req.Method == http.MethodPost {
		return fmt.Errorf("%s 缺失；请前往小云雀官网个人设置页创建 Access Key，地址：https://xyq.jianying.com/home?tab_name=home\n配置后重试：\n  export %s=\"<your-access-key>\"", config.EnvXYQAccessKey, config.EnvXYQAccessKey)
	}
	if accessKey != "" {
		req.Header.Set("Authorization", "Bearer "+accessKey)
	}
	return nil
}
