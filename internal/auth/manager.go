package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type Manager struct {
	cfg                   *config.Config
	store                 CredentialStore
	httpClient            *http.Client
	authBaseURL           *url.URL
	random                io.Reader
	now                   func() time.Time
	credentialMu          sync.Mutex
	cachedCredential      *Credential
	credentialCacheLoaded bool
}

type ManagerOption func(*Manager)

func WithCredentialStore(store CredentialStore) ManagerOption {
	return func(manager *Manager) {
		if store != nil {
			manager.store = store
		}
	}
}

func WithHTTPClient(client *http.Client) ManagerOption {
	return func(manager *Manager) {
		if client != nil {
			manager.httpClient = client
		}
	}
}

func withAuthBaseURLForTest(rawURL string) ManagerOption {
	return func(manager *Manager) {
		parsed, err := url.Parse(rawURL)
		if err == nil {
			manager.authBaseURL = parsed
		}
	}
}

func withRandomReaderForTest(reader io.Reader) ManagerOption {
	return func(manager *Manager) {
		if reader != nil {
			manager.random = reader
		}
	}
}

func withClockForTest(now func() time.Time) ManagerOption {
	return func(manager *Manager) {
		if now != nil {
			manager.now = now
		}
	}
}

// NewManager always uses the canonical production auth origin. cfg.BaseURL and
// cfg.PPEEnv intentionally do not affect browser grants, login cookies, or AK
// provisioning; PPE routing applies only after a dedicated AK has been issued.
func NewManager(cfg *config.Config, options ...ManagerOption) *Manager {
	serviceName := config.DefaultAuthStoreServiceName
	authBaseURL, _ := url.Parse(config.DefaultBaseURL)
	timeout := config.DefaultHTTPTimeout
	if cfg != nil && cfg.HTTPTimeout > 0 {
		timeout = cfg.HTTPTimeout
	}
	manager := &Manager{
		cfg:         cfg,
		store:       NewDefaultCredentialStore(serviceName),
		httpClient:  &http.Client{Timeout: timeout},
		authBaseURL: authBaseURL,
		random:      rand.Reader,
		now:         time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
	}
	return manager
}

func (m *Manager) ResolveAccessKey(ctx context.Context) (string, error) {
	if m != nil && m.cfg != nil {
		if accessKey := strings.TrimSpace(m.cfg.AccessKey); accessKey != "" {
			return accessKey, nil
		}
	}
	credential, err := m.loadCredential(ctx)
	if err != nil {
		return "", err
	}
	if credential.AccessKey == "" {
		return "", ErrCredentialNotFound
	}
	if credential.ExpiredAt <= m.now().Add(m.ensureTTL()).Unix() {
		return "", ErrCredentialExpired
	}
	return credential.AccessKey, nil
}

func (m *Manager) Login(ctx context.Context, options LoginOptions) (*Credential, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	identity, err := m.ensureIdentity(ctx)
	if err != nil {
		return nil, err
	}
	flow, err := startBrowserFlow(m.authBaseURL, m.random)
	if err != nil {
		return nil, err
	}
	defer flow.close()

	writeProgress(options.Progress, "正在打开小云雀网页授权…")
	opener := options.OpenURL
	if opener == nil {
		opener = OpenBrowser
	}
	if err := opener(flow.loginURL); err != nil {
		return nil, errors.New("无法自动打开浏览器，请检查系统默认浏览器设置后重试")
	}
	writeProgress(options.Progress, "请在浏览器中完成登录和授权，CLI 会自动继续…")

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultLoginTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload, err := flow.wait(waitCtx)
	if err != nil {
		return nil, err
	}
	writeProgress(options.Progress, "网页授权已完成，正在准备本机专属 CLI 凭证…")
	credential, err := m.exchangeAndProvision(waitCtx, payload, identity, options)
	if err != nil {
		return nil, err
	}
	if err := m.saveCredential(waitCtx, credential); err != nil {
		return nil, err
	}
	writeProgress(options.Progress, "小云雀 CLI 登录成功。")
	return cloneCredential(credential), nil
}

func (m *Manager) Status(ctx context.Context) (*Status, error) {
	if m != nil && m.cfg != nil && strings.TrimSpace(m.cfg.AccessKey) != "" {
		return &Status{LoggedIn: true, Source: "environment"}, nil
	}
	credential, err := m.loadCredential(ctx)
	if errors.Is(err, ErrCredentialNotFound) {
		return &Status{}, nil
	}
	if err != nil {
		return nil, err
	}
	status := &Status{
		Source:          "browser",
		UID:             credential.UID,
		TokenID:         credential.TokenID,
		CredentialScope: credential.CredentialScope,
	}
	if credential.ExpiredAt > 0 {
		status.ExpiresAt = time.Unix(credential.ExpiredAt, 0)
	}
	status.LoggedIn = credential.AccessKey != "" && credential.ExpiredAt > m.now().Add(m.ensureTTL()).Unix()
	return status, nil
}

func (m *Manager) Logout(ctx context.Context, revoke bool) error {
	if revoke {
		// The current AK management endpoint requires a browser/team session and
		// cannot be safely called using the Access Key that would be revoked.
		return ErrRemoteRevokeUnsupported
	}
	credential, err := m.loadCredential(ctx)
	if errors.Is(err, ErrCredentialNotFound) {
		m.clearCredentialCache()
		return nil
	}
	if err != nil {
		return err
	}
	// Keep the non-secret device identity so a later login can safely reuse the
	// same remote token instead of consuming another per-user AK slot. Logout
	// only clears the local secret and account binding; it does not revoke the
	// remote Access Key.
	if err := m.saveCredential(ctx, identityOnly(credential)); err != nil {
		return err
	}
	m.clearCredentialCache()
	return nil
}

func (m *Manager) CredentialScope(ctx context.Context) (string, error) {
	credential, err := m.loadCredential(ctx)
	if err != nil {
		return "", err
	}
	if credential.AccessKey == "" || strings.TrimSpace(credential.UID) == "" {
		return "", ErrCredentialNotFound
	}
	return credentialScope(credential.UID, credential.DeviceID), nil
}

func (m *Manager) ensureIdentity(ctx context.Context) (*Credential, error) {
	credential, err := m.loadCredential(ctx)
	if err == nil {
		return credential, nil
	}
	if !errors.Is(err, ErrCredentialNotFound) {
		return nil, err
	}
	identity, err := newIdentity(m.random)
	if err != nil {
		return nil, err
	}
	if err := m.saveCredential(ctx, identity); err != nil {
		return nil, err
	}
	return identity, nil
}

func (m *Manager) loadCredential(ctx context.Context) (*Credential, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	m.credentialMu.Lock()
	defer m.credentialMu.Unlock()
	if m.credentialCacheLoaded {
		if m.cachedCredential == nil {
			return nil, ErrCredentialNotFound
		}
		return cloneCredential(m.cachedCredential), nil
	}
	credential, err := m.store.Load(ctx)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			m.credentialCacheLoaded = true
			m.cachedCredential = nil
		}
		return nil, err
	}
	credential = normalizeCredential(credential)
	m.cachedCredential = cloneCredential(credential)
	m.credentialCacheLoaded = true
	return cloneCredential(credential), nil
}

func (m *Manager) saveCredential(ctx context.Context, credential *Credential) error {
	if err := m.validate(); err != nil {
		return err
	}
	credential = normalizeCredential(credential)
	if err := m.store.Save(ctx, credential); err != nil {
		m.clearCredentialCache()
		return err
	}
	m.credentialMu.Lock()
	m.cachedCredential = cloneCredential(credential)
	m.credentialCacheLoaded = true
	m.credentialMu.Unlock()
	return nil
}

func (m *Manager) clearCredentialCache() {
	if m == nil {
		return
	}
	m.credentialMu.Lock()
	m.cachedCredential = nil
	m.credentialCacheLoaded = false
	m.credentialMu.Unlock()
}

func normalizeCredential(credential *Credential) *Credential {
	credential = cloneCredential(credential)
	if credential == nil {
		return nil
	}
	if credential.AccessKey == "" {
		credential.CredentialScope = ""
		return credential
	}
	credential.CredentialScope = credentialScope(credential.UID, credential.DeviceID)
	return credential
}

func (m *Manager) validate() error {
	if m == nil || m.store == nil || m.httpClient == nil || m.authBaseURL == nil || m.random == nil || m.now == nil {
		return errors.New("小云雀 CLI 授权管理器未正确初始化")
	}
	if m.authBaseURL.Scheme != "https" && !(m.authBaseURL.Scheme == "http" && isLoopbackHost(m.authBaseURL.Hostname())) {
		return errors.New("小云雀授权地址必须使用 HTTPS")
	}
	if m.authBaseURL.Host == "" || m.authBaseURL.RawQuery != "" || m.authBaseURL.Fragment != "" {
		return errors.New("小云雀授权地址无效")
	}
	return nil
}

func (m *Manager) ensureTTL() time.Duration {
	if m != nil && m.cfg != nil && m.cfg.AuthTTL > 0 {
		return m.cfg.AuthTTL
	}
	return config.DefaultAuthTTL
}

func isLoopbackHost(host string) bool {
	return host == "127.0.0.1" || strings.EqualFold(host, "localhost") || host == "::1"
}

func writeProgress(writer io.Writer, message string) {
	if writer != nil {
		_, _ = fmt.Fprintln(writer, message)
	}
}

func cloneCredential(credential *Credential) *Credential {
	if credential == nil {
		return nil
	}
	copy := *credential
	return &copy
}
