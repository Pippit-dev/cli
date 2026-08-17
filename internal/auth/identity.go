package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

func randomEncoded(reader io.Reader, size int) (string, error) {
	if reader == nil {
		reader = rand.Reader
	}
	value := make([]byte, size)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func randomTempName() (string, error) {
	value, err := randomEncoded(rand.Reader, 18)
	if err != nil {
		return "", err
	}
	return ".credential-" + value + ".tmp", nil
}

func validDeviceID(deviceID string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(deviceID)
	return err == nil && len(decoded) == deviceIDBytes &&
		constantTimeEqual(deviceID, base64.RawURLEncoding.EncodeToString(decoded))
}

func credentialScope(uid, deviceID string) string {
	return "pippit-tool-cli:user:" + accountBinding(uid) + ":device:" + deviceID
}

func accountBinding(uid string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(uid)))
	return base64.RawURLEncoding.EncodeToString(digest[:16])
}

func accountBindingFromCredentialScope(scope, deviceID string) (string, bool) {
	prefix := "pippit-tool-cli:user:"
	suffix := ":device:" + deviceID
	if !strings.HasPrefix(scope, prefix) || !strings.HasSuffix(scope, suffix) {
		return "", false
	}
	binding := strings.TrimSuffix(strings.TrimPrefix(scope, prefix), suffix)
	return binding, validAccountBinding(binding)
}

func legacyCredentialScope(deviceID string) string {
	return "pippit-tool-cli:device:" + deviceID
}

func constantTimeEqual(left, right string) bool {
	leftHash := sha256.Sum256([]byte(left))
	rightHash := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

func newIdentity(reader io.Reader) (*Credential, error) {
	deviceID, err := randomEncoded(reader, deviceIDBytes)
	if err != nil {
		return nil, errors.New("生成本机登录设备标识失败")
	}
	return &Credential{
		Version:  credentialVersion,
		DeviceID: deviceID,
	}, nil
}

func identityOnly(credential *Credential) *Credential {
	if credential == nil {
		return nil
	}
	return &Credential{
		Version:  credential.Version,
		DeviceID: credential.DeviceID,
		// TokenID is a non-secret exact selector used by the Web page to reuse the
		// same remote token after logout instead of consuming another AK slot.
		TokenID: credential.TokenID,
	}
}
