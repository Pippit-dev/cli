package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	defaultKeyringAccount = "browser-login"
	credentialFileName    = "browser-credential.json"
	maxCredentialBytes    = 64 << 10
)

type keyringBackend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (systemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (systemKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

type keyringCredentialStore struct {
	backend keyringBackend
	service string
	account string
}

func (s *keyringCredentialStore) Load(ctx context.Context) (*Credential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := s.backend.Get(s.service, s.account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("读取系统钥匙串失败: %w", ErrSecureStore)
	}
	credential, err := decodeCredential([]byte(value))
	if err != nil {
		return nil, err
	}
	return credential, nil
}

func (s *keyringCredentialStore) Save(ctx context.Context, credential *Credential) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := encodeCredential(credential)
	if err != nil {
		return err
	}
	if err := s.backend.Set(s.service, s.account, string(payload)); err != nil {
		return fmt.Errorf("写入系统钥匙串失败: %w", ErrSecureStore)
	}
	return nil
}

func (s *keyringCredentialStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.backend.Delete(s.service, s.account)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrCredentialNotFound
	}
	if err != nil {
		return fmt.Errorf("删除系统钥匙串凭证失败: %w", ErrSecureStore)
	}
	return nil
}

type resilientCredentialStore struct {
	primary  CredentialStore
	fallback CredentialStore
}

type storedCredential struct {
	Version         int    `json:"version"`
	DeviceID        string `json:"device_id"`
	CredentialScope string `json:"credential_scope"`
	TokenName       string `json:"token_name"`
	AccessKey       string `json:"access_key,omitempty"`
	TokenID         string `json:"token_id,omitempty"`
	UID             string `json:"uid,omitempty"`
	ExpiredAt       int64  `json:"expired_at,omitempty"`
}

// NewDefaultCredentialStore prefers the operating system keyring. Unix uses
// a private no-follow file as a fallback; Windows deliberately has no file
// fallback because an equivalent ACL guarantee is not provided here.
func NewDefaultCredentialStore(serviceName string) CredentialStore {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		serviceName = "pippit-cli"
	}
	primary := &keyringCredentialStore{
		backend: systemKeyring{},
		service: serviceName,
		account: defaultKeyringAccount,
	}
	return &resilientCredentialStore{
		primary:  primary,
		fallback: newPlatformFallbackStore(),
	}
}

func (s *resilientCredentialStore) Load(ctx context.Context) (*Credential, error) {
	credential, primaryErr := s.primary.Load(ctx)
	if primaryErr == nil {
		return credential, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !errors.Is(primaryErr, ErrCredentialNotFound) && !errors.Is(primaryErr, ErrSecureStore) {
		// A decodable-but-invalid primary record may indicate corruption or
		// tampering. Never mask it with an older fallback credential.
		return nil, primaryErr
	}
	if s.fallback == nil {
		if errors.Is(primaryErr, ErrCredentialNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, primaryErr
	}
	credential, fallbackErr := s.fallback.Load(ctx)
	if fallbackErr == nil {
		return credential, nil
	}
	if errors.Is(primaryErr, ErrCredentialNotFound) && errors.Is(fallbackErr, ErrCredentialNotFound) {
		return nil, ErrCredentialNotFound
	}
	if !errors.Is(fallbackErr, ErrCredentialNotFound) {
		return nil, fallbackErr
	}
	return nil, primaryErr
}

func (s *resilientCredentialStore) Save(ctx context.Context, credential *Credential) error {
	primaryErr := s.primary.Save(ctx, credential)
	if primaryErr == nil {
		if s.fallback != nil {
			if err := ignoreNotFound(s.fallback.Delete(ctx)); err != nil {
				return fmt.Errorf("系统钥匙串已更新，但清理旧的备用凭证失败: %w", err)
			}
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !errors.Is(primaryErr, ErrCredentialNotFound) && !errors.Is(primaryErr, ErrSecureStore) {
		return primaryErr
	}
	if s.fallback == nil {
		return primaryErr
	}
	if err := s.fallback.Save(ctx, credential); err != nil {
		return err
	}
	return nil
}

func (s *resilientCredentialStore) Delete(ctx context.Context) error {
	primaryErr := ignoreNotFound(s.primary.Delete(ctx))
	var fallbackErr error
	if s.fallback != nil {
		fallbackErr = ignoreNotFound(s.fallback.Delete(ctx))
	}
	if primaryErr != nil {
		return primaryErr
	}
	return fallbackErr
}

func ignoreNotFound(err error) error {
	if errors.Is(err, ErrCredentialNotFound) {
		return nil
	}
	return err
}

func encodeCredential(credential *Credential) ([]byte, error) {
	if err := validateCredential(credential); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(storedCredential{
		Version:         credential.Version,
		DeviceID:        credential.DeviceID,
		CredentialScope: credential.CredentialScope,
		TokenName:       credential.TokenName,
		AccessKey:       credential.AccessKey,
		TokenID:         credential.TokenID,
		UID:             credential.UID,
		ExpiredAt:       credential.ExpiredAt,
	})
	if err != nil {
		return nil, errors.New("编码本机登录凭证失败")
	}
	return payload, nil
}

func decodeCredential(payload []byte) (*Credential, error) {
	if len(payload) == 0 || len(payload) > maxCredentialBytes {
		return nil, errors.New("本机登录凭证格式无效")
	}
	record := &storedCredential{}
	if err := json.Unmarshal(payload, record); err != nil {
		return nil, errors.New("本机登录凭证格式无效")
	}
	credential := &Credential{
		Version:         record.Version,
		DeviceID:        record.DeviceID,
		CredentialScope: record.CredentialScope,
		TokenName:       record.TokenName,
		AccessKey:       record.AccessKey,
		TokenID:         record.TokenID,
		UID:             record.UID,
		ExpiredAt:       record.ExpiredAt,
	}
	if err := validateCredential(credential); err != nil {
		return nil, err
	}
	return credential, nil
}

func defaultCredentialPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		return ""
	}
	return filepath.Join(dir, "pippit-cli", credentialFileName)
}

type unavailableCredentialStore struct{}

func (unavailableCredentialStore) Load(context.Context) (*Credential, error) {
	return nil, ErrSecureStore
}

func (unavailableCredentialStore) Save(context.Context, *Credential) error {
	return ErrSecureStore
}

func (unavailableCredentialStore) Delete(context.Context) error {
	return ErrSecureStore
}

func validateCredential(credential *Credential) error {
	if credential == nil {
		return errors.New("本机登录凭证不能为空")
	}
	if credential.Version != credentialVersion {
		return errors.New("本机登录凭证版本不受支持")
	}
	if !validDeviceID(credential.DeviceID) {
		return errors.New("本机登录设备标识无效")
	}
	if !constantTimeEqual(credential.TokenName, tokenNameForDevice(credential.DeviceID)) {
		return errors.New("本机登录凭证作用域无效")
	}
	if credential.AccessKey == "" {
		if strings.TrimSpace(credential.TokenID) != credential.TokenID || credential.UID != "" || credential.ExpiredAt != 0 ||
			(credential.CredentialScope != "" && !constantTimeEqual(credential.CredentialScope, legacyCredentialScope(credential.DeviceID))) {
			return errors.New("本机登录凭证不完整")
		}
		return nil
	}
	if strings.TrimSpace(credential.AccessKey) != credential.AccessKey ||
		credential.TokenID == "" || credential.UID == "" || credential.ExpiredAt <= 0 {
		return errors.New("本机登录凭证不完整")
	}
	expectedScope := credentialScope(credential.UID, credential.DeviceID)
	if !constantTimeEqual(credential.CredentialScope, expectedScope) &&
		!constantTimeEqual(credential.CredentialScope, legacyCredentialScope(credential.DeviceID)) {
		return errors.New("本机登录凭证作用域无效")
	}
	return nil
}
