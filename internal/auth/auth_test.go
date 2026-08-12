package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

type memoryCredentialStore struct {
	mu         sync.Mutex
	credential *Credential
	loadErr    error
	saveErr    error
	deleteErr  error
	loads      int
	saves      int
	deletes    int
}

func (s *memoryCredentialStore) Load(ctx context.Context) (*Credential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	if s.credential == nil {
		return nil, ErrCredentialNotFound
	}
	return cloneCredential(s.credential), nil
}

func (s *memoryCredentialStore) Save(ctx context.Context, credential *Credential) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.credential = cloneCredential(credential)
	return nil
}

func (s *memoryCredentialStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	if s.credential == nil {
		return ErrCredentialNotFound
	}
	s.credential = nil
	return nil
}

func TestBrowserFlowUsesDedicatedPageAndStrictBoundCallback(t *testing.T) {
	authURL, _ := url.Parse(config.DefaultBaseURL)
	deviceID, _ := randomEncoded(bytes.NewReader(bytes.Repeat([]byte{0x21}, deviceIDBytes)), deviceIDBytes)
	flow, err := startBrowserFlow(
		authURL, bytes.NewReader(bytes.Repeat([]byte{0x42}, 2*randomBindingBytes)),
		deviceID, "saved-token-id", accountBinding("saved-user"), true,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer flow.close()

	loginURL, err := url.Parse(flow.loginURL)
	if err != nil {
		t.Fatal(err)
	}
	if loginURL.Scheme+"://"+loginURL.Host != config.DefaultBaseURL || loginURL.Path != loginPagePath {
		t.Fatalf("login URL target = %s://%s%s", loginURL.Scheme, loginURL.Host, loginURL.Path)
	}
	query := loginURL.Query()
	if query.Get("source") != loginSource || query.Get("device_id") != deviceID || query.Get("force") != "1" ||
		query.Get("token_id") != "saved-token-id" || query.Get("expected_account") != accountBinding("saved-user") {
		t.Fatalf("login URL public binding is incomplete: %v", query)
	}
	for _, field := range []string{"random_secret_key"} {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(query.Get(field))
		if decodeErr != nil || len(decoded) != randomBindingBytes || len(query.Get(field)) != 43 {
			t.Fatalf("%s is not canonical 32-byte Base64URL", field)
		}
	}
	callbackURL := query.Get("callback")
	parsedCallback, err := url.Parse(callbackURL)
	if err != nil {
		t.Fatal(err)
	}
	state := parsedCallback.Query().Get("state")
	decodedState, decodeErr := base64.RawURLEncoding.DecodeString(state)
	if parsedCallback.Scheme != "http" || parsedCallback.Hostname() != "127.0.0.1" || parsedCallback.Path != callbackPath ||
		decodeErr != nil || len(decodedState) != randomBindingBytes || len(state) != 43 {
		t.Fatalf("callback is not canonical: %q", callbackURL)
	}

	payload := accessKeyPayload{
		Type:            "access_key",
		AccessKey:       "callback-ak-secret",
		UID:             "12345",
		TokenID:         "token-id",
		ExpiredAt:       time.Now().Add(time.Hour).Unix(),
		RandomSecretKey: query.Get("random_secret_key"),
		Source:          loginSource,
		CallbackURL:     callbackURL,
	}
	if status := sendCallback(t, callbackURL, "https://evil.example", payload); status != http.StatusForbidden {
		t.Fatalf("wrong origin status = %d", status)
	}
	wrongSecret := payload
	wrongSecret.RandomSecretKey = "wrong-secret"
	if status := sendCallback(t, callbackURL, config.DefaultBaseURL, wrongSecret); status != http.StatusBadRequest {
		t.Fatalf("wrong secret status = %d", status)
	}
	if status := postRawCallback(t, callbackURL, config.DefaultBaseURL, `{"type":"access_key","unknown":true}`); status != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", status)
	}
	if status := sendCallback(t, callbackURL+"&state=duplicate", config.DefaultBaseURL, payload); status != http.StatusBadRequest {
		t.Fatalf("duplicate state status = %d", status)
	}
	if status := sendCallback(t, callbackURL+"&unexpected=value", config.DefaultBaseURL, payload); status != http.StatusBadRequest {
		t.Fatalf("unexpected transport query status = %d", status)
	}
	if status := sendPreflight(t, callbackURL, config.DefaultBaseURL); status != http.StatusNoContent {
		t.Fatalf("preflight status = %d", status)
	}
	securityRuntimeCallback := callbackURL + "&a_bogus=signed-request&msToken=transport-token"
	if status := sendCallback(t, securityRuntimeCallback, config.DefaultBaseURL, payload); status != http.StatusOK {
		t.Fatalf("security-runtime callback status = %d", status)
	}
	got, err := flow.wait(context.Background())
	if err != nil || got.AccessKey != payload.AccessKey || got.UID != payload.UID || got.TokenID != payload.TokenID {
		t.Fatalf("callback payload = %#v, %v", credentialPayloadWithoutSecret(got), err)
	}
	if status := sendCallback(t, callbackURL, config.DefaultBaseURL, payload); status != http.StatusConflict {
		t.Fatalf("replayed callback status = %d", status)
	}
}

func TestManagerLoginStoresPageIssuedCredentialWithoutServerExchange(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	store := &memoryCredentialStore{}
	cfg := config.Load()
	cfg.AccessKey = ""
	manager := NewManager(
		cfg,
		WithCredentialStore(store),
		withRandomReaderForTest(bytes.NewReader(bytes.Repeat([]byte{0x31}, deviceIDBytes+2*randomBindingBytes))),
		withClockForTest(func() time.Time { return fixedNow }),
	)
	var progress bytes.Buffer
	var callbackSecret string
	credential, err := manager.Login(context.Background(), LoginOptions{
		Progress: &progress,
		OpenURL: func(rawURL string) error {
			loginURL, parseErr := url.Parse(rawURL)
			if parseErr != nil {
				return parseErr
			}
			if loginURL.Path != loginPagePath || loginURL.Query().Get("force") != "" {
				return fmt.Errorf("unexpected login URL")
			}
			callbackSecret = loginURL.Query().Get("random_secret_key")
			payload := accessKeyPayload{
				Type:            "access_key",
				AccessKey:       "page-issued-ak",
				UID:             "user-100",
				TokenID:         "ak-id-100",
				ExpiredAt:       fixedNow.Add(time.Hour).Unix(),
				RandomSecretKey: callbackSecret,
				Source:          loginSource,
				CallbackURL:     loginURL.Query().Get("callback"),
			}
			if status := sendCallback(t, payload.CallbackURL, config.DefaultBaseURL, payload); status != http.StatusOK {
				return fmt.Errorf("callback status %d", status)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessKey != "page-issued-ak" || credential.UID != "user-100" || credential.TokenID != "ak-id-100" ||
		credential.CredentialScope != credentialScope("user-100", credential.DeviceID) {
		t.Fatalf("credential = %#v", credentialWithoutSecret(credential))
	}
	store.mu.Lock()
	stored := cloneCredential(store.credential)
	store.mu.Unlock()
	if stored == nil || stored.AccessKey != credential.AccessKey {
		t.Fatalf("stored credential = %#v", credentialWithoutSecret(stored))
	}
	for _, secret := range []string{credential.AccessKey, callbackSecret} {
		if strings.Contains(progress.String(), secret) {
			t.Fatalf("progress leaked a secret: %q", progress.String())
		}
	}
}

func TestManagerReauthenticationPinsAccountAndRequiresRotation(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	old, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{0x33}, deviceIDBytes)))
	old.AccessKey = "rejected-ak"
	old.TokenID = "rejected-id"
	old.UID = "account-a"
	old.ExpiredAt = fixedNow.Add(time.Hour).Unix()
	old.CredentialScope = credentialScope(old.UID, old.DeviceID)

	t.Run("different account is not saved", func(t *testing.T) {
		store := &memoryCredentialStore{credential: cloneCredential(old)}
		manager := NewManager(config.Load(), WithCredentialStore(store), withClockForTest(func() time.Time { return fixedNow }))
		_, err := manager.Login(context.Background(), LoginOptions{
			ForceRefresh:            true,
			ExpectedCredentialScope: old.CredentialScope,
			OpenURL:                 callbackOpener(t, fixedNow.Add(time.Hour), "account-b", "new-id", "new-ak", true),
		})
		if !errors.Is(err, ErrCredentialAccountMismatch) {
			t.Fatalf("mismatch error = %v", err)
		}
		if store.credential.AccessKey != old.AccessKey || store.saves != 0 {
			t.Fatal("account mismatch overwrote the stored credential")
		}
	})

	t.Run("same rejected token is refused", func(t *testing.T) {
		store := &memoryCredentialStore{credential: cloneCredential(old)}
		manager := NewManager(config.Load(), WithCredentialStore(store), withClockForTest(func() time.Time { return fixedNow }))
		_, err := manager.Login(context.Background(), LoginOptions{
			ForceRefresh:            true,
			ExpectedCredentialScope: old.CredentialScope,
			OpenURL:                 callbackOpener(t, fixedNow.Add(time.Hour), old.UID, old.TokenID, old.AccessKey, true),
		})
		if err == nil || !strings.Contains(err.Error(), "未轮换") {
			t.Fatalf("same-token force login error = %v", err)
		}
		if strings.Contains(err.Error(), old.AccessKey) {
			t.Fatal("force-login error leaked the rejected Access Key")
		}
	})

	t.Run("rotated token is accepted", func(t *testing.T) {
		store := &memoryCredentialStore{credential: cloneCredential(old)}
		manager := NewManager(config.Load(), WithCredentialStore(store), withClockForTest(func() time.Time { return fixedNow }))
		got, err := manager.Login(context.Background(), LoginOptions{
			ForceRefresh:            true,
			ExpectedCredentialScope: old.CredentialScope,
			OpenURL:                 callbackOpener(t, fixedNow.Add(time.Hour), old.UID, "rotated-id", "rotated-ak", true),
		})
		if err != nil || got.AccessKey != "rotated-ak" || got.CredentialScope != old.CredentialScope {
			t.Fatalf("rotated credential = %#v, %v", credentialWithoutSecret(got), err)
		}
	})
}

func TestBrowserFlowRequiresExpectedAccountForForceAndNeverUsesRawUID(t *testing.T) {
	authURL, _ := url.Parse(config.DefaultBaseURL)
	deviceID, _ := randomEncoded(bytes.NewReader(bytes.Repeat([]byte{0x22}, deviceIDBytes)), deviceIDBytes)
	if _, err := startBrowserFlow(
		authURL, bytes.NewReader(bytes.Repeat([]byte{0x42}, 2*randomBindingBytes)),
		deviceID, "saved-token-id", "", true,
	); err == nil || !strings.Contains(err.Error(), "登录账号") {
		t.Fatalf("force flow without account binding error = %v", err)
	}

	const rawUID = "raw-user-id-must-not-be-in-url"
	flow, err := startBrowserFlow(
		authURL, bytes.NewReader(bytes.Repeat([]byte{0x43}, 2*randomBindingBytes)),
		deviceID, "saved-token-id", accountBinding(rawUID), true,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer flow.close()
	if strings.Contains(flow.loginURL, rawUID) {
		t.Fatal("login URL exposed raw UID")
	}
	parsed, err := url.Parse(flow.loginURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("expected_account"); got != accountBinding(rawUID) || !validAccountBinding(got) {
		t.Fatalf("expected_account = %q", got)
	}
}

func TestBrowserFlowSendsStoredTokenIDOnNormalLogin(t *testing.T) {
	authURL, _ := url.Parse(config.DefaultBaseURL)
	deviceID, _ := randomEncoded(bytes.NewReader(bytes.Repeat([]byte{0x23}, deviceIDBytes)), deviceIDBytes)
	flow, err := startBrowserFlow(
		authURL, bytes.NewReader(bytes.Repeat([]byte{0x44}, 2*randomBindingBytes)),
		deviceID, "existing-token-id", accountBinding("existing-user"), false,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer flow.close()
	parsed, err := url.Parse(flow.loginURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("token_id") != "existing-token-id" || parsed.Query().Get("force") != "" {
		t.Fatalf("normal login query = %v", parsed.Query())
	}
}

func TestBrowserFlowAllowsLogoutIdentityTokenWithoutAccountBinding(t *testing.T) {
	authURL, _ := url.Parse(config.DefaultBaseURL)
	deviceID, _ := randomEncoded(bytes.NewReader(bytes.Repeat([]byte{0x24}, deviceIDBytes)), deviceIDBytes)
	flow, err := startBrowserFlow(
		authURL, bytes.NewReader(bytes.Repeat([]byte{0x45}, 2*randomBindingBytes)),
		deviceID, "existing-token-id", "", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer flow.close()
	parsed, err := url.Parse(flow.loginURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("token_id") != "existing-token-id" || parsed.Query().Get("expected_account") != "" ||
		parsed.Query().Get("force") != "" {
		t.Fatalf("logout relogin query = %v", parsed.Query())
	}
}

func TestManagerRejectsExpiredPageCredential(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	store := &memoryCredentialStore{}
	manager := NewManager(config.Load(), WithCredentialStore(store), withClockForTest(func() time.Time { return fixedNow }))
	_, err := manager.Login(context.Background(), LoginOptions{
		OpenURL: callbackOpener(t, fixedNow.Add(-time.Hour), "user", "id", "ak", false),
	})
	if !errors.Is(err, ErrCredentialExpired) {
		t.Fatalf("expired callback error = %v", err)
	}
	// The fresh device identity is safe to retain, but the expired AK must not be saved.
	if store.credential == nil || store.credential.AccessKey != "" {
		t.Fatalf("expired callback was saved: %#v", credentialWithoutSecret(store.credential))
	}
}

func TestManagerDerivesExpectedAccountFromDurableScope(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{0x25}, deviceIDBytes)))
	identity.TokenID = "saved-token-id"
	store := &memoryCredentialStore{credential: identity}
	manager := NewManager(config.Load(), WithCredentialStore(store), withClockForTest(func() time.Time { return fixedNow }))
	expectedScope := credentialScope("durable-user", identity.DeviceID)
	_, err := manager.Login(context.Background(), LoginOptions{
		ForceRefresh:            true,
		ExpectedCredentialScope: expectedScope,
		OpenURL: func(rawURL string) error {
			loginURL, parseErr := url.Parse(rawURL)
			if parseErr != nil {
				return parseErr
			}
			if got := loginURL.Query().Get("expected_account"); got != accountBinding("durable-user") ||
				loginURL.Query().Get("force") != "1" || loginURL.Query().Get("token_id") != identity.TokenID ||
				strings.Contains(rawURL, "durable-user") {
				return fmt.Errorf("durable reauth binding is incomplete")
			}
			payload := accessKeyPayload{
				Type:            "access_key",
				AccessKey:       "new-ak",
				UID:             "durable-user",
				TokenID:         "rotated-token-id",
				ExpiredAt:       fixedNow.Add(time.Hour).Unix(),
				RandomSecretKey: loginURL.Query().Get("random_secret_key"),
				Source:          loginSource,
				CallbackURL:     loginURL.Query().Get("callback"),
			}
			if status := sendCallback(t, payload.CallbackURL, config.DefaultBaseURL, payload); status != http.StatusOK {
				return fmt.Errorf("callback status %d", status)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolveStatusLogoutAndAuthOrigin(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	credential, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{3}, deviceIDBytes)))
	credential.AccessKey = "stored-ak"
	credential.TokenID = "stored-id"
	credential.UID = "789"
	credential.CredentialScope = credentialScope(credential.UID, credential.DeviceID)
	credential.ExpiredAt = fixedNow.Add(time.Hour).Unix()
	store := &memoryCredentialStore{credential: credential}
	cfg := config.Load()
	cfg.AccessKey = " env-ak "
	cfg.BaseURL = "https://untrusted.invalid"
	cfg.PPEEnv = "ppe_untrusted"
	manager := NewManager(cfg, WithCredentialStore(store), withClockForTest(func() time.Time { return fixedNow }))
	if manager.authBaseURL.String() != config.DefaultBaseURL {
		t.Fatalf("browser login followed runtime PPE/base URL: %q", manager.authBaseURL)
	}
	if key, err := manager.ResolveAccessKey(context.Background()); err != nil || key != "env-ak" {
		t.Fatalf("environment precedence = %q, %v", key, err)
	}
	status, err := manager.Status(context.Background())
	if err != nil || !status.LoggedIn || status.Source != "environment" {
		t.Fatalf("environment status = %#v, %v", status, err)
	}

	cfg.AccessKey = ""
	if key, err := manager.ResolveAccessKey(context.Background()); err != nil || key != credential.AccessKey {
		t.Fatalf("stored resolution = %q, %v", key, err)
	}
	if scope, err := manager.CredentialScope(context.Background()); err != nil || scope != credential.CredentialScope {
		t.Fatalf("credential scope = %q, %v", scope, err)
	}
	if err := manager.Logout(context.Background(), true); !errors.Is(err, ErrRemoteRevokeUnsupported) {
		t.Fatalf("remote revoke error = %v", err)
	}
	if err := manager.Logout(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if store.credential == nil || store.credential.DeviceID != credential.DeviceID || store.credential.AccessKey != "" ||
		store.credential.UID != "" || store.credential.TokenID != credential.TokenID {
		t.Fatalf("logout did not retain only device identity: %#v", credentialWithoutSecret(store.credential))
	}
	if _, err := manager.ResolveAccessKey(context.Background()); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("post-logout resolution = %v", err)
	}
}

func TestDecodeCredentialAcceptsLegacyTokenName(t *testing.T) {
	deviceID, _ := randomEncoded(bytes.NewReader(bytes.Repeat([]byte{0x44}, deviceIDBytes)), deviceIDBytes)
	uid := "legacy-user"
	payload, err := json.Marshal(map[string]any{
		"version":          credentialVersion,
		"device_id":        deviceID,
		"credential_scope": credentialScope(uid, deviceID),
		"token_name":       "pippit-tool-cli-legacy-name",
		"access_key":       "legacy-ak",
		"token_id":         "legacy-id",
		"uid":              uid,
		"expired_at":       time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := decodeCredential(payload)
	if err != nil || credential.AccessKey != "legacy-ak" || credential.DeviceID != deviceID {
		t.Fatalf("legacy credential = %#v, %v", credentialWithoutSecret(credential), err)
	}
	encoded, err := encodeCredential(credential)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("token_name")) {
		t.Fatalf("new credential encoding retained legacy field: %s", encoded)
	}
}

func TestManagerNormalizesLegacyDeviceScopeOnLoad(t *testing.T) {
	credential, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{0x45}, deviceIDBytes)))
	credential.AccessKey = "legacy-ak"
	credential.TokenID = "legacy-id"
	credential.UID = "legacy-user"
	credential.ExpiredAt = time.Now().Add(time.Hour).Unix()
	credential.CredentialScope = legacyCredentialScope(credential.DeviceID)
	payload, err := encodeCredential(credential)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeCredential(payload)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryCredentialStore{credential: decoded}
	manager := NewManager(config.Load(), WithCredentialStore(store))
	got, err := manager.loadCredential(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.CredentialScope != credentialScope(credential.UID, credential.DeviceID) {
		t.Fatalf("legacy scope was not normalized: %q", got.CredentialScope)
	}
}

func TestManagerCredentialCacheIsConcurrent(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	credential, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{3}, deviceIDBytes)))
	credential.AccessKey = "stored-ak"
	credential.TokenID = "stored-id"
	credential.UID = "789"
	credential.CredentialScope = credentialScope(credential.UID, credential.DeviceID)
	credential.ExpiredAt = fixedNow.Add(time.Hour).Unix()
	store := &memoryCredentialStore{credential: credential}
	cfg := config.Load()
	cfg.AccessKey = ""
	manager := NewManager(cfg, WithCredentialStore(store), withClockForTest(func() time.Time { return fixedNow }))

	const callers = 24
	start := make(chan struct{})
	errCh := make(chan error, callers)
	var group sync.WaitGroup
	for index := 0; index < callers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			key, err := manager.ResolveAccessKey(context.Background())
			if err == nil && key != credential.AccessKey {
				err = fmt.Errorf("unexpected access key")
			}
			errCh <- err
		}()
	}
	close(start)
	group.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if store.loads != 1 {
		t.Fatalf("credential store loads = %d, want 1", store.loads)
	}
}

func TestResilientStoreFallbackBoundaries(t *testing.T) {
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{4}, deviceIDBytes)))
	primary := &memoryCredentialStore{loadErr: ErrSecureStore, saveErr: ErrSecureStore, deleteErr: ErrSecureStore}
	fallback := &memoryCredentialStore{}
	store := &resilientCredentialStore{primary: primary, fallback: fallback}
	if err := store.Save(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded.DeviceID != identity.DeviceID {
		t.Fatalf("fallback load = %#v, %v", credentialWithoutSecret(loaded), err)
	}
	if err := store.Delete(context.Background()); !errors.Is(err, ErrSecureStore) {
		t.Fatalf("primary delete failure was hidden: %v", err)
	}

	corrupt := errors.New("corrupt primary credential")
	primary = &memoryCredentialStore{loadErr: corrupt}
	fallback = &memoryCredentialStore{credential: identity}
	store = &resilientCredentialStore{primary: primary, fallback: fallback}
	if _, err := store.Load(context.Background()); !errors.Is(err, corrupt) || fallback.loads != 0 {
		t.Fatalf("corrupt primary was masked: err=%v fallback-loads=%d", err, fallback.loads)
	}
}

func TestFileCredentialStoreIsPrivateAtomicAndNoFollow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows intentionally uses the system keyring without a file fallback")
	}
	directory := filepath.Join(t.TempDir(), "auth")
	path := filepath.Join(directory, credentialFileName)
	store := NewFileCredentialStore(path)
	credential, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{5}, deviceIDBytes)))
	credential.AccessKey = "file-ak"
	credential.TokenID = "file-id"
	credential.UID = "100"
	credential.CredentialScope = credentialScope(credential.UID, credential.DeviceID)
	credential.ExpiredAt = time.Now().Add(time.Hour).Unix()
	if err := store.Save(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file mode = %v, %v", info, err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded.AccessKey != credential.AccessKey {
		t.Fatalf("loaded credential = %#v, %v", credentialWithoutSecret(loaded), err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("must-not-change"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, ErrSecureStore) {
		t.Fatalf("symlink load error = %v", err)
	}
	if err := store.Save(context.Background(), credential); err != nil {
		t.Fatal(err)
	}
	targetData, _ := os.ReadFile(target)
	if string(targetData) != "must-not-change" {
		t.Fatal("atomic save followed the symlink target")
	}
}

func TestSanitizedBrowserEnv(t *testing.T) {
	input := []string{
		"PATH=/usr/bin",
		"XYQ_ACCESS_KEY=secret",
		"PIPPIT_TOKEN=secret",
		"PIPPIT_CLI_AK=secret",
		"PIPPIT_CLI_PPE_ENV=ppe_safe",
		"OTHER_TOKEN=unrelated",
	}
	joined := strings.Join(SanitizedBrowserEnv(input), "\n")
	for _, forbidden := range []string{"XYQ_ACCESS_KEY", "PIPPIT_TOKEN", "PIPPIT_CLI_AK"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("browser env retained %s", forbidden)
		}
	}
	for _, wanted := range []string{"PATH=/usr/bin", "PIPPIT_CLI_PPE_ENV=ppe_safe", "OTHER_TOKEN=unrelated"} {
		if !strings.Contains(joined, wanted) {
			t.Fatalf("browser env removed %s", wanted)
		}
	}
}

func callbackOpener(t *testing.T, expiry time.Time, uid, tokenID, accessKey string, wantForce bool) func(string) error {
	t.Helper()
	return func(rawURL string) error {
		loginURL, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		if (loginURL.Query().Get("force") == "1") != wantForce {
			return fmt.Errorf("force query mismatch")
		}
		if wantForce && loginURL.Query().Get("token_id") == "" {
			return fmt.Errorf("force login omitted token_id")
		}
		if wantForce && loginURL.Query().Get("expected_account") == "" {
			return fmt.Errorf("force login omitted expected_account")
		}
		payload := accessKeyPayload{
			Type:            "access_key",
			AccessKey:       accessKey,
			UID:             uid,
			TokenID:         tokenID,
			ExpiredAt:       expiry.Unix(),
			RandomSecretKey: loginURL.Query().Get("random_secret_key"),
			Source:          loginSource,
			CallbackURL:     loginURL.Query().Get("callback"),
		}
		if status := sendCallback(t, payload.CallbackURL, config.DefaultBaseURL, payload); status != http.StatusOK {
			return fmt.Errorf("callback status %d", status)
		}
		return nil
	}
}

func sendCallback(t *testing.T, callbackURL, origin string, payload accessKeyPayload) int {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return postRawCallback(t, callbackURL, origin, string(body))
}

func postRawCallback(t *testing.T, callbackURL, origin, body string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, callbackURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func sendPreflight(t *testing.T, callbackURL, origin string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodOptions, callbackURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Access-Control-Request-Method", "POST")
	request.Header.Set("Access-Control-Request-Headers", "Content-Type")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func credentialWithoutSecret(credential *Credential) any {
	if credential == nil {
		return nil
	}
	return struct {
		Version         int
		DeviceID        string
		CredentialScope string
		TokenID         string
		UID             string
		ExpiredAt       int64
	}{credential.Version, credential.DeviceID, credential.CredentialScope, credential.TokenID, credential.UID, credential.ExpiredAt}
}

func credentialPayloadWithoutSecret(payload accessKeyPayload) any {
	return struct {
		Type      string
		UID       string
		TokenID   string
		ExpiredAt int64
		Source    string
	}{payload.Type, payload.UID, payload.TokenID, payload.ExpiredAt, payload.Source}
}
