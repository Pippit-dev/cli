package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
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

func tokenNameForDevice(deviceID string) string {
	digest := sha256.Sum256([]byte(deviceID))
	return "pippit-tool-cli-" + base64.RawURLEncoding.EncodeToString(digest[:16])
}

func credentialScope(uid, deviceID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(uid)))
	return "pippit-tool-cli:user:" + base64.RawURLEncoding.EncodeToString(digest[:16]) + ":device:" + deviceID
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
		Version:   credentialVersion,
		DeviceID:  deviceID,
		TokenName: tokenNameForDevice(deviceID),
	}, nil
}

func identityOnly(credential *Credential) *Credential {
	if credential == nil {
		return nil
	}
	return &Credential{
		Version:   credential.Version,
		DeviceID:  credential.DeviceID,
		TokenName: credential.TokenName,
		// TokenID is not an authentication secret. Keeping this exact remote
		// reference lets a later login reuse or rotate the same device token
		// without consuming another per-account AK slot.
		TokenID: credential.TokenID,
	}
}

func redactedOperationError(operation string) error {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "授权操作"
	}
	return fmt.Errorf("%s失败，请稍后重试", operation)
}
