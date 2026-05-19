package common

import (
	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

// Runner carries runtime dependencies for command execution.
type Runner struct {
	Config         *config.Config
	Client         Client
	AuthAuthorizer auth.Authorizer
}

func NewRunner(cfg *config.Config, client Client, authAuthorizer auth.Authorizer) *Runner {
	return &Runner{
		Config:         cfg,
		Client:         client,
		AuthAuthorizer: authAuthorizer,
	}
}
