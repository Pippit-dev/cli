package internal

import "github.com/Pippit-dev/pippit-cli/internal/config"

// Runner carries runtime dependencies for command execution.
type Runner struct {
	Config *config.Config
	Client *Client
}

func NewRunner(cfg *config.Config) *Runner {
	return &Runner{
		Config: cfg,
		Client: NewClient(cfg.BaseURL, cfg.HTTPTimeout),
	}
}
