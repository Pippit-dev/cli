package common

import (
	"context"

	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/Pippit-dev/pippit-cli/internal/config"
)

// AuthManager is the credential boundary shared by native CLI commands. It
// resolves explicit CI credentials first and browser-managed credentials
// second, without exposing either value to command output.
type AuthManager interface {
	ResolveAccessKey(context.Context) (string, error)
	Login(context.Context, auth.LoginOptions) (*auth.Credential, error)
	Status(context.Context) (*auth.Status, error)
	Logout(context.Context, bool) error
	CredentialScope(context.Context) (string, error)
}

// Runner carries runtime dependencies for command execution.
type Runner struct {
	Config *config.Config
	Client Client
	Auth   AuthManager
}

func NewRunner(cfg *config.Config, client Client) *Runner {
	return &Runner{
		Config: cfg,
		Client: client,
	}
}
