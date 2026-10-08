// Package adapter defines the CLI command and HTTP request policy interfaces.
// Different runtimes can implement them without depending on a specific platform or command framework.
package adapter

import (
	"context"
	"net/http"
)

type Adapter interface {
	CLIAdapter
	HTTPAdapter
}

type CLIAdapter interface {
	CLIOptions() CLIOptions
}

type HTTPAdapter interface {
	// Called only after the common client confirms the API origin matches. The client owns the request, so its path and authentication headers may be changed.
	PrepareAPIRequest(context.Context, *http.Request) (AuthDecision, error)
	// Wraps the HTTP transport. A nil next uses the standard default transport.
	// Returning nil lets http.Client use its default transport.
	WrapTransport(next http.RoundTripper) http.RoundTripper
	// Removes sensitive request headers used by this implementation. The common client handles Authorization removal and origin checks.
	StripSensitiveHeaders(*http.Request)
}

type CLIOptions struct {
	// Whether to allow top-level business commands by default. Local authentication and self-update have separate switches.
	DefaultAllowRootCommands bool
	// When commands are denied by default, this allowlist enables them. Nil and empty slices both mean no commands are allowed.
	AllowedRootCommands     []string
	LocalCredentialsEnabled bool
	SelfUpdateEnabled       bool
	// An empty string keeps the common help text.
	RootHelpOverride string
}

type AuthDecision uint8

const (
	UseDefaultAuth AuthDecision = iota
	// AuthProvided means authentication material was attached, not that the server has authenticated the request.
	AuthProvided
)

// Default allows all commands, local authentication, and self-update. HTTP requests use the client's default authentication and transport.
type Default struct{}

var _ Adapter = Default{}

func (Default) CLIOptions() CLIOptions {
	return CLIOptions{DefaultAllowRootCommands: true, LocalCredentialsEnabled: true, SelfUpdateEnabled: true}
}
func (Default) PrepareAPIRequest(context.Context, *http.Request) (AuthDecision, error) {
	return UseDefaultAuth, nil
}
func (Default) WrapTransport(next http.RoundTripper) http.RoundTripper { return next }
func (Default) StripSensitiveHeaders(*http.Request)                    {}

func OrDefault(value Adapter) Adapter {
	if value == nil {
		return Default{}
	}
	return value
}
func HTTPOrDefault(value HTTPAdapter) HTTPAdapter {
	if value == nil {
		return Default{}
	}
	return value
}
