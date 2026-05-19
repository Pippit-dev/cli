package common

import (
	"context"
	"net/http"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type Authorizer interface {
	Refresh(ctx context.Context, ensureTTL time.Duration) error
	Inject(ctx context.Context, req *http.Request) error
}

// Runner carries runtime dependencies for command execution.
type Runner struct {
	Config     *config.Config
	Client     *Client
	Authorizer Authorizer
}

func NewRunner(cfg *config.Config) *Runner {
	return &Runner{
		Config: cfg,
		Client: NewClient(cfg.BaseURL, cfg.HTTPTimeout),
	}
}
