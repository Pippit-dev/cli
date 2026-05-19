package common

import (
	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

// Runner carries runtime dependencies for command execution.
type Runner struct {
	Config         *config.Config
	Client         *Client
	AuthAuthorizer auth.Authorizer
}

func NewRunner(cfg *config.Config) *Runner {
	authManager := auth.NewManager(cfg)
	return &Runner{
		Config:         cfg,
		Client:         NewClient(cfg.BaseURL, cfg.HTTPTimeout),
		AuthAuthorizer: authManager,
	}
}
