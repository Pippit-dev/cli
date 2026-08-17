//go:build windows

package auth

func NewFileCredentialStore(string) CredentialStore {
	return unavailableCredentialStore{}
}

func newPlatformFallbackStore() CredentialStore {
	return nil
}
