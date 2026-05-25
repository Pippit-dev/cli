//go:build ignore

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"code.byted.org/passport/auth_client/go/authsdk"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type Authorizer interface {
	Refresh(ctx context.Context, ensureTTL time.Duration) error
	Inject(ctx context.Context, req *http.Request) error
	NewLoginFlow(ctx context.Context) (*LoginFlow, error)
	CheckLogin(ctx context.Context, deviceCode string) (*State, error)
	State(ctx context.Context) (*State, error)
	Logout(ctx context.Context) error
}

type OAuthManager struct {
	cfg    *config.Config
	mu     sync.Mutex
	client *authsdk.Client
}

type LoginFlow struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
}

type State struct {
	LoggedIn  bool      `json:"logged_in"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

func NewManager(cfg *config.Config) *OAuthManager {
	return &OAuthManager{cfg: cfg}
}

func (m *OAuthManager) NewLoginFlow(ctx context.Context) (*LoginFlow, error) {
	client, err := m.clientInstance()
	if err != nil {
		return nil, err
	}
	flow, err := client.Authenticator().NewLoginFlow(ctx)
	if err != nil {
		return nil, err
	}
	return &LoginFlow{
		DeviceCode:      flow.DeviceCode,
		UserCode:        flow.UserCode,
		VerificationURI: flow.VerificationURI,
	}, nil
}

func (m *OAuthManager) CheckLogin(ctx context.Context, deviceCode string) (*State, error) {
	client, err := m.clientInstance()
	if err != nil {
		return nil, err
	}
	state, err := client.Authenticator().CheckLogin(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	return authState(state), nil
}

func (m *OAuthManager) State(ctx context.Context) (*State, error) {
	client, err := m.clientInstance()
	if err != nil {
		return nil, err
	}
	state, err := client.Authorizer().State(ctx)
	if err != nil {
		return nil, err
	}
	return authState(state), nil
}

func (m *OAuthManager) Refresh(ctx context.Context, ensureTTL time.Duration) error {
	client, err := m.clientInstance()
	if err != nil {
		return err
	}
	_, err = client.Authorizer().Refresh(ctx, ensureTTL)
	return err
}

func (m *OAuthManager) Inject(ctx context.Context, req *http.Request) error {
	client, err := m.clientInstance()
	if err != nil {
		return err
	}
	return client.Authorizer().Inject(ctx, req)
}

func (m *OAuthManager) Logout(ctx context.Context) error {
	client, err := m.clientInstance()
	if err != nil {
		return err
	}
	return client.Authorizer().Logout(ctx)
}

func IsLoginPending(err error) bool {
	return errors.Is(err, authsdk.ErrLoginPending)
}

func (m *OAuthManager) clientInstance() (*authsdk.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		return m.client, nil
	}
	if m.cfg == nil || m.cfg.OAuth == nil {
		return nil, errors.New("oauth config is required")
	}
	oauth := m.cfg.OAuth
	client, err := authsdk.NewClient(authsdk.Config{
		ClientKey:        oauth.ClientKey,
		BaseURL:          oauth.BaseURL,
		StoreServiceName: oauth.StoreServiceName,
		Scopes:           oauth.Scopes,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize oauth client: %w", err)
	}
	m.client = client
	return client, nil
}

func authState(state *authsdk.AuthStateView) *State {
	if state == nil {
		return &State{}
	}
	return &State{
		LoggedIn:  state.LoggedIn,
		ExpiresAt: state.ExpiresAt,
	}
}
