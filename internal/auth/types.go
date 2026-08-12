package auth

import (
	"context"
	"errors"
	"io"
	"time"
)

const (
	loginPagePath      = "/cli/pippit-tool-login"
	callbackPath       = "/pippit-tool/callback"
	loginSource        = "pippit-tool-cli"
	credentialVersion  = 1
	deviceIDBytes      = 32
	randomBindingBytes = 32

	DefaultLoginTimeout = 5 * time.Minute
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
	// ForceRefresh asks the browser page to rotate this device's rejected AK.
	// The CLI never calls QueryAk, DeleteAk, or GenerateAk itself.
	ForceRefresh bool
	// ExpectedCredentialScope binds reauthentication to the UID and device that
	// started a durable operation. A different browser account fails before the
	// returned Access Key is saved.
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
