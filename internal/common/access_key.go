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
		return fmt.Errorf("%s is required for authenticated requests", config.EnvXYQAccessKey)
	}
	if a.accessKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.accessKey)
	}
	return nil
}
