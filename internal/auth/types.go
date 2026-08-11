package auth

import (
	"context"
	"errors"
	"io"
	"time"
)

const (
	loginExportPath       = "/cli/login-export"
	callbackPath          = "/xyq/callback/save_session"
	exchangeGrantPath     = "/api/web/v1/auth/exchange_cli_login_grant"
	queryAccessKeyPath    = "/api/biz/v1/user/query_ak"
	generateAccessKeyPath = "/api/biz/v1/user/generate_ak"
	deleteAccessKeyPath   = "/api/biz/v1/user/delete_ak"
	loginSource           = "pippit-tool-cli"
	credentialVersion     = 1
	deviceIDBytes         = 32
	randomBindingBytes    = 32

	DefaultLoginTimeout       = 5 * time.Minute
	DefaultCredentialLifetime = 365 * 24 * time.Hour
)

var (
	ErrCredentialNotFound        = errors.New("未找到本机小云雀 CLI 登录凭证")
	ErrCredentialExpired         = errors.New("本机小云雀 CLI 登录凭证已过期")
	ErrSecureStore               = errors.New("安全凭证存储不可用")
	ErrCredentialAccountMismatch = errors.New("网页授权账号与当前任务账号不一致")
	ErrRemoteRevokeUnsupported   = errors.New("当前版本不支持在 CLI 中安全撤销远程 Access Key")
)

// Credential is the dedicated, device-scoped Access Key managed by this CLI.
// AccessKey is secret and must never be printed, logged, or written to journals.
type Credential struct {
	Version         int    `json:"version"`
	DeviceID        string `json:"device_id"`
	CredentialScope string `json:"credential_scope"`
	TokenName       string `json:"token_name"`
	AccessKey       string `json:"-"`
	TokenID         string `json:"token_id,omitempty"`
	UID             string `json:"uid,omitempty"`
	ExpiredAt       int64  `json:"expired_at,omitempty"`
}

// CredentialStore persists a credential without exposing its serialized form.
type CredentialStore interface {
	Load(context.Context) (*Credential, error)
	Save(context.Context, *Credential) error
	Delete(context.Context) error
}

type LoginOptions struct {
	// OpenURL should open the URL without logging it. When nil, the platform's
	// standard browser opener is used with credential-bearing env vars removed.
	OpenURL  func(string) error
	Progress io.Writer
	Timeout  time.Duration
	// ForceRefresh rotates every remote token owned by this device identity
	// before provisioning a replacement. It is used after an explicit 401 so
	// a successful browser login can never return the just-rejected AK again.
	ForceRefresh bool
	// ExpectedCredentialScope binds reauthentication to the UID and device that
	// started a durable operation. A different browser account fails before any
	// Access Key is deleted or generated.
	ExpectedCredentialScope string
}

type Status struct {
	LoggedIn        bool      `json:"logged_in"`
	Source          string    `json:"source,omitempty"`
	UID             string    `json:"uid,omitempty"`
	TokenID         string    `json:"token_id,omitempty"`
	CredentialScope string    `json:"credential_scope,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
}
