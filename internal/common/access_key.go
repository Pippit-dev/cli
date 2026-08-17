package common

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type accessKeyAuthorizer struct {
	accessKey func(context.Context) (string, error)
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
	return NewAccessKeyContextProviderAuthorizer(func(context.Context) (string, error) {
		return accessKey(), nil
	})
}

// NewAccessKeyContextProviderAuthorizer resolves a credential lazily for each
// request. This keeps keychain access out of --help and lets a browser login in
// the same process authorize the next request without rebuilding the client.
func NewAccessKeyContextProviderAuthorizer(accessKey func(context.Context) (string, error)) RequestAuthorizer {
	if accessKey == nil {
		accessKey = func(context.Context) (string, error) { return "", nil }
	}
	return &accessKeyAuthorizer{accessKey: accessKey}
}

func (a *accessKeyAuthorizer) Inject(ctx context.Context, req *http.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	accessKey := ""
	if a != nil && a.accessKey != nil {
		resolved, err := a.accessKey(ctx)
		if err != nil && req.Method == http.MethodPost {
			if errors.Is(err, internal_auth.ErrCredentialNotFound) || errors.Is(err, internal_auth.ErrCredentialExpired) {
				return fmt.Errorf("未找到可用的小云雀登录凭证；请先运行 pippit-tool-cli login: %w", err)
			}
			return fmt.Errorf("读取小云雀 CLI 安全登录凭证失败；请检查本机凭证库后重试: %w", err)
		}
		accessKey = strings.TrimSpace(resolved)
	}
	if accessKey == "" && req.Method == http.MethodPost {
		return fmt.Errorf("未登录小云雀 CLI；请先运行 pippit-tool-cli login（CI 仍可设置 %s）", config.EnvXYQAccessKey)
	}
	if accessKey != "" {
		req.Header.Set("Authorization", "Bearer "+accessKey)
	}
	return nil
}
