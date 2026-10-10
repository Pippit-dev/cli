package auth

import (
	"context"
	"errors"

	"github.com/Pippit-dev/pippit-cli/internal/space"
)

// SpaceAccount uses the verified browser UID, stable across AK rotation.
func (m *Manager) SpaceAccount(ctx context.Context) (string, error) {
	// Re-read at request boundaries so an account switch in another process
	// cannot keep using this process's cached identity or credential.
	m.clearCredentialCache()
	credential, err := m.loadCredential(ctx)
	if errors.Is(err, ErrCredentialNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if credential.UID == "" || credential.AccessKey == "" {
		return "", nil
	}
	return "user:" + space.Digest(credential.UID), nil
}
