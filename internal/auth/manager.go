package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type Manager struct {
	cfg                   *config.Config
	store                 CredentialStore
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

// NewManager always uses the canonical production login page. Runtime business
// API configuration does not affect the browser identity origin.
func NewManager(cfg *config.Config, options ...ManagerOption) *Manager {
	serviceName := config.DefaultAuthStoreServiceName
	authBaseURL, _ := url.Parse(config.DefaultBaseURL)
	manager := &Manager{
		cfg:         cfg,
		store:       NewDefaultCredentialStore(serviceName),
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
	forceRefresh := options.ForceRefresh && strings.TrimSpace(identity.TokenID) != ""
	expectedAccount := ""
	if identity.UID != "" {
		expectedAccount = accountBinding(identity.UID)
	}
	if expectedScope := strings.TrimSpace(options.ExpectedCredentialScope); expectedScope != "" {
		scopeAccount, ok := accountBindingFromCredentialScope(expectedScope, identity.DeviceID)
		if !ok || (expectedAccount != "" && !constantTimeEqual(expectedAccount, scopeAccount)) {
			return nil, ErrCredentialAccountMismatch
		}
		expectedAccount = scopeAccount
	}
	flow, err := startBrowserFlow(
		m.authBaseURL, m.random, identity.DeviceID, identity.TokenID, expectedAccount, forceRefresh,
	)
	if err != nil {
		return nil, err
	}
	defer flow.close()

	writeProgress(options.Progress, "小云雀网页授权地址（如未自动打开，请复制到浏览器）：")
	writeProgress(options.Progress, flow.loginURL)
	writeProgress(options.Progress, "正在尝试自动打开浏览器…")
	opener := options.OpenURL
	if opener == nil {
		opener = OpenBrowser
	}
	if err := opener(flow.loginURL); err != nil {
		writeProgress(options.Progress, "未能自动打开浏览器，请复制上方授权地址到浏览器继续；CLI 将继续等待授权回调…")
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
	writeProgress(options.Progress, "网页授权已完成，正在安全保存本机 CLI 凭证…")
	credential := credentialFromCallback(identity, payload)
	if credential.ExpiredAt <= m.now().Add(m.ensureTTL()).Unix() {
		return nil, ErrCredentialExpired
	}
	if expected := strings.TrimSpace(options.ExpectedCredentialScope); expected != "" &&
		!constantTimeEqual(expected, credential.CredentialScope) {
		return nil, ErrCredentialAccountMismatch
	}
	if forceRefresh &&
		((identity.AccessKey != "" && constantTimeEqual(identity.AccessKey, credential.AccessKey)) ||
			constantTimeEqual(identity.TokenID, credential.TokenID)) {
		return nil, errors.New("网页授权未轮换已失效的 CLI Access Key，已拒绝继续使用旧凭证")
	}
	if err := validateCredential(credential); err != nil {
		return nil, errors.New("网页返回的 CLI 凭证格式无效")
	}
	if err := m.saveCredential(waitCtx, credential); err != nil {
		return nil, err
	}
	writeProgress(options.Progress, "小云雀 CLI 登录成功。")
	return cloneCredential(credential), nil
}

func credentialFromCallback(identity *Credential, payload accessKeyPayload) *Credential {
	credential := cloneCredential(identity)
	credential.AccessKey = payload.AccessKey
	credential.TokenID = payload.TokenID
	credential.UID = payload.UID
	credential.ExpiredAt = payload.ExpiredAt
	credential.CredentialScope = credentialScope(payload.UID, identity.DeviceID)
	return credential
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
	if m == nil || m.store == nil || m.authBaseURL == nil || m.random == nil || m.now == nil {
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
