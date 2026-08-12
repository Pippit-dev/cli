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
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func TestBrowserFlowRequiresExactBoundCallbackAndCORS(t *testing.T) {
	authURL, _ := url.Parse("https://xyq.jianying.com")
	flow, err := startBrowserFlow(authURL, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	defer flow.close()

	loginURL, err := url.Parse(flow.loginURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := loginURL.Query().Get("source"); got != loginSource {
		t.Fatalf("source = %q, want %q", got, loginSource)
	}
	if got := loginURL.Query().Get("ppe_env"); got != "" {
		t.Fatalf("ppe_env = %q, want absent", got)
	}
	for _, name := range []string{"random_secret_key"} {
		decoded, err := base64.RawURLEncoding.DecodeString(loginURL.Query().Get(name))
		if err != nil || len(decoded) < randomBindingBytes {
			t.Fatalf("%s is not at least %d random bytes", name, randomBindingBytes)
		}
	}
	callback, err := url.Parse(flow.callbackURL)
	if err != nil {
		t.Fatal(err)
	}
	decodedState, err := base64.RawURLEncoding.DecodeString(callback.Query().Get("state"))
	if err != nil || len(decodedState) < randomBindingBytes {
		t.Fatalf("state is not at least %d random bytes", randomBindingBytes)
	}

	preflight, _ := http.NewRequest(http.MethodOptions, flow.callbackURL, nil)
	preflight.Header.Set("Origin", "https://xyq.jianying.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "content-type")
	preflightResponse, err := http.DefaultClient.Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	preflightResponse.Body.Close()
	if preflightResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d", preflightResponse.StatusCode)
	}
	if got := preflightResponse.Header.Get("Access-Control-Allow-Origin"); got != "https://xyq.jianying.com" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := preflightResponse.Header.Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("allow private network = %q", got)
	}

	payload := loginGrantPayload{
		Type:            "login_grant",
		Grant:           "one-time-grant",
		RandomSecretKey: flow.secret,
		Source:          flow.source,
		CallbackURL:     flow.callbackURL,
	}
	wrong := payload
	wrong.Source = "other-cli"
	if status := postCallback(t, flow.callbackURL, flow.origin, wrong); status != http.StatusBadRequest {
		t.Fatalf("wrong binding status = %d", status)
	}
	wrongStateURL := *callback
	wrongStateQuery := wrongStateURL.Query()
	wrongStateQuery.Set("state", "wrong-state")
	wrongStateURL.RawQuery = wrongStateQuery.Encode()
	if status := postCallback(t, wrongStateURL.String(), flow.origin, payload); status != http.StatusBadRequest {
		t.Fatalf("wrong state status = %d", status)
	}
	if status := postCallback(t, flow.callbackURL, "https://attacker.invalid", payload); status != http.StatusForbidden {
		t.Fatalf("wrong origin status = %d", status)
	}
	if status := postCallback(t, flow.callbackURL, flow.origin, payload); status != http.StatusOK {
		t.Fatalf("valid callback status = %d", status)
	}
	got, err := flow.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Grant != payload.Grant {
		t.Fatalf("grant = %q", got.Grant)
	}
}

func TestManagerLoginReusesOnlyExactDeviceToken(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	store := &memoryCredentialStore{}
	var generated bool
	var expectedName string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assertAuthHeaders(t, request)
		if request.Header.Get("x-use-ppe") != "" || request.Header.Get("x-tt-env") != "" {
			t.Errorf("auth request unexpectedly carried PPE headers")
		}
		switch request.URL.Path {
		case exchangeGrantPath:
			var body map[string]string
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["grant"] != "grant-value" || body["random_secret_key"] == "" {
				t.Errorf("unexpected exchange body")
			}
			http.SetCookie(writer, &http.Cookie{Name: "session", Value: "cookie-secret", Path: "/", Secure: true, HttpOnly: true})
			writeEnvelope(writer, map[string]any{"uid": "123", "scope": loginGrantScope})
		case queryAccessKeyPath:
			cookie, err := request.Cookie("session")
			if err != nil || cookie.Value != "cookie-secret" {
				t.Errorf("query did not receive temporary exchange cookie")
			}
			credential, err := store.Load(context.Background())
			if err != nil {
				t.Errorf("load identity: %v", err)
				return
			}
			expectedName = credential.TokenName
			writeEnvelope(writer, map[string]any{"access_token_list": []map[string]any{
				{"ak_id": "foreign-id", "token": "foreign-ak", "expired_at": fixedNow.Add(time.Hour).Unix(), "token_name": "someone-else", "token_status": "enable"},
				{"ak_id": "managed-id", "token": "managed-ak", "expired_at": fixedNow.Add(time.Hour).Unix(), "token_name": credential.TokenName, "token_status": "enable"},
			}})
		case generateAccessKeyPath:
			generated = true
			http.Error(writer, "must not generate", http.StatusInternalServerError)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	cfg := config.Load()
	cfg.AccessKey = ""
	cfg.PPEEnv = "ppe_must_not_reach_auth"
	manager := NewManager(cfg,
		WithCredentialStore(store),
		WithHTTPClient(server.Client()),
		withAuthBaseURLForTest(server.URL),
		withClockForTest(func() time.Time { return fixedNow }),
	)
	var progress bytes.Buffer
	callbackResult := make(chan error, 1)
	credential, err := manager.Login(context.Background(), LoginOptions{
		Timeout:  2 * time.Second,
		Progress: &progress,
		OpenURL: func(rawURL string) error {
			loginURL, err := url.Parse(rawURL)
			if err != nil {
				return err
			}
			if loginURL.Scheme != "https" || loginURL.Host != strings.TrimPrefix(server.URL, "https://") {
				t.Errorf("login URL origin = %s://%s", loginURL.Scheme, loginURL.Host)
			}
			if loginURL.Query().Get("ppe_env") != "" {
				t.Error("login URL carried PPE lane")
			}
			payload := loginGrantPayload{
				Type:            "login_grant",
				Grant:           "grant-value",
				RandomSecretKey: loginURL.Query().Get("random_secret_key"),
				Source:          loginSource,
				CallbackURL:     loginURL.Query().Get("callback"),
			}
			go func() {
				status, err := sendCallback(payload.CallbackURL, server.URL, payload)
				if err == nil && status != http.StatusOK {
					err = errors.New("callback did not return HTTP 200")
				}
				callbackResult <- err
			}()
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-callbackResult; err != nil {
		t.Fatal(err)
	}
	if generated {
		t.Fatal("generate endpoint was called despite exact valid token")
	}
	if credential.AccessKey != "managed-ak" || credential.TokenID != "managed-id" || credential.UID != "123" {
		t.Fatalf("credential metadata = %#v", credentialWithoutSecret(credential))
	}
	if credential.TokenName != expectedName || len(credential.TokenName) > 48 {
		t.Fatalf("token name = %q", credential.TokenName)
	}
	publicJSON, err := json.Marshal(credential)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publicJSON), credential.AccessKey) {
		t.Fatal("credential JSON exposed the Access Key")
	}
	if strings.Contains(progress.String(), "managed-ak") || strings.Contains(progress.String(), "grant-value") || strings.Contains(progress.String(), "cookie-secret") {
		t.Fatalf("progress leaked credentials: %q", progress.String())
	}
}

func TestExchangeGeneratesWhenExactValidTokenDoesNotExist(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	identity, err := newIdentity(bytes.NewReader(bytes.Repeat([]byte{7}, deviceIDBytes)))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assertAuthHeaders(t, request)
		switch request.URL.Path {
		case exchangeGrantPath:
			http.SetCookie(writer, &http.Cookie{Name: "session", Value: "session-value", Path: "/", Secure: true})
			writeEnvelope(writer, map[string]any{"uid": "456", "scope": loginGrantScope})
		case queryAccessKeyPath:
			writeEnvelope(writer, map[string]any{"access_token_list": []map[string]any{
				{"ak_id": "foreign-id", "token": "foreign-ak", "expired_at": fixedNow.Add(time.Hour).Unix(), "token_name": "foreign", "token_status": "enable"},
				{"ak_id": "expired-id", "token": "expired-ak", "expired_at": fixedNow.Add(-time.Hour).Unix(), "token_name": identity.TokenName, "token_status": "enable"},
			}})
		case generateAccessKeyPath:
			var body struct {
				TokenName string `json:"token_name"`
				ExpiredAt int64  `json:"expired_at"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.TokenName != identity.TokenName {
				t.Errorf("generated token name = %q", body.TokenName)
			}
			if body.ExpiredAt != fixedNow.Add(DefaultCredentialLifetime).Unix() {
				t.Errorf("generated expiry = %d", body.ExpiredAt)
			}
			writeEnvelope(writer, map[string]any{"ak": "new-managed-ak", "token_id": "new-managed-id"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	manager := NewManager(config.Load(),
		WithCredentialStore(&memoryCredentialStore{}),
		WithHTTPClient(server.Client()),
		withAuthBaseURLForTest(server.URL),
		withClockForTest(func() time.Time { return fixedNow }),
	)
	credential, err := manager.exchangeAndProvision(context.Background(), loginGrantPayload{
		Grant: "grant", RandomSecretKey: "secret", ExpireAt: fixedNow.Add(time.Minute).Unix(),
	}, identity, LoginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessKey != "new-managed-ak" || credential.TokenID != "new-managed-id" || credential.UID != "456" {
		t.Fatalf("credential metadata = %#v", credentialWithoutSecret(credential))
	}
}

func TestForceRefreshDeletesExactDeviceTokensBeforeGeneratingReplacement(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{8}, deviceIDBytes)))
	identity.AccessKey = "rejected-ak"
	identity.TokenID = "rejected-id"
	identity.UID = "456"
	identity.CredentialScope = credentialScope(identity.UID, identity.DeviceID)
	identity.ExpiredAt = fixedNow.Add(time.Hour).Unix()

	var calls []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls = append(calls, request.URL.Path)
		switch request.URL.Path {
		case exchangeGrantPath:
			http.SetCookie(writer, &http.Cookie{Name: "session", Value: "session-value", Path: "/", Secure: true})
			writeEnvelope(writer, map[string]any{"uid": identity.UID, "scope": loginGrantScope})
		case queryAccessKeyPath:
			writeEnvelope(writer, map[string]any{"access_token_list": []map[string]any{
				{"ak_id": identity.TokenID, "token": identity.AccessKey, "expired_at": identity.ExpiredAt, "token_name": "user-renamed-token", "token_status": "enable"},
				{"ak_id": "stale-duplicate", "token": "stale-ak", "expired_at": identity.ExpiredAt, "token_name": identity.TokenName, "token_status": "disable"},
				{"ak_id": "foreign-id", "token": "foreign-ak", "expired_at": identity.ExpiredAt, "token_name": "another-device", "token_status": "enable"},
			}})
		case deleteAccessKeyPath:
			var body struct {
				AKIDs []string `json:"ak_ids"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if strings.Join(body.AKIDs, ",") != "rejected-id" {
				t.Errorf("deleted IDs = %v", body.AKIDs)
			}
			writeEnvelope(writer, map[string]any{})
		case generateAccessKeyPath:
			writeEnvelope(writer, map[string]any{"ak": "replacement-ak", "token_id": "replacement-id"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	manager := NewManager(config.Load(), WithHTTPClient(server.Client()), withAuthBaseURLForTest(server.URL), withClockForTest(func() time.Time { return fixedNow }))
	credential, err := manager.exchangeAndProvision(context.Background(), loginGrantPayload{
		Grant: "grant", RandomSecretKey: "secret", ExpireAt: fixedNow.Add(time.Minute).Unix(),
	}, identity, LoginOptions{ForceRefresh: true, ExpectedCredentialScope: identity.CredentialScope})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessKey != "replacement-ak" || credential.TokenID != "replacement-id" {
		t.Fatalf("replacement credential = %#v", credentialWithoutSecret(credential))
	}
	if got := strings.Join(calls, ","); got != exchangeGrantPath+","+queryAccessKeyPath+","+deleteAccessKeyPath+","+generateAccessKeyPath {
		t.Fatalf("endpoint order = %q", got)
	}
}

func TestForceRefreshRejectsDifferentBrowserAccountBeforeAKMutation(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{6}, deviceIDBytes)))
	identity.AccessKey = "account-a-ak"
	identity.TokenID = "account-a-id"
	identity.UID = "account-a"
	identity.CredentialScope = credentialScope(identity.UID, identity.DeviceID)
	identity.ExpiredAt = fixedNow.Add(time.Hour).Unix()
	mutated := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != exchangeGrantPath {
			mutated = true
			http.Error(writer, "unexpected", http.StatusInternalServerError)
			return
		}
		http.SetCookie(writer, &http.Cookie{Name: "session", Value: "session-value", Path: "/", Secure: true})
		writeEnvelope(writer, map[string]any{"uid": "account-b", "scope": loginGrantScope})
	}))
	defer server.Close()
	manager := NewManager(config.Load(), WithHTTPClient(server.Client()), withAuthBaseURLForTest(server.URL), withClockForTest(func() time.Time { return fixedNow }))
	_, err := manager.exchangeAndProvision(context.Background(), loginGrantPayload{
		Grant: "grant", RandomSecretKey: "secret", ExpireAt: fixedNow.Add(time.Minute).Unix(),
	}, identity, LoginOptions{ForceRefresh: true, ExpectedCredentialScope: identity.CredentialScope})
	if !errors.Is(err, ErrCredentialAccountMismatch) || mutated {
		t.Fatalf("error/mutated = %v/%v, want account mismatch before AK mutation", err, mutated)
	}
}

func TestForceRefreshNeverDeletesStoredTokenAfterAccountSwitch(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{6}, deviceIDBytes)))
	identity.AccessKey = "account-a-ak"
	identity.TokenID = "shared-looking-id"
	identity.UID = "account-a"
	identity.CredentialScope = credentialScope(identity.UID, identity.DeviceID)
	identity.ExpiredAt = fixedNow.Add(time.Hour).Unix()
	deleteCalled := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case exchangeGrantPath:
			http.SetCookie(writer, &http.Cookie{Name: "session", Value: "session-value", Path: "/", Secure: true})
			writeEnvelope(writer, map[string]any{"uid": "account-b", "scope": loginGrantScope})
		case queryAccessKeyPath:
			writeEnvelope(writer, map[string]any{"access_token_list": []map[string]any{{
				"ak_id": identity.TokenID, "token": "account-b-ak", "expired_at": identity.ExpiredAt,
				"token_name": identity.TokenName, "token_status": "enable",
			}}})
		case deleteAccessKeyPath:
			deleteCalled = true
			http.Error(writer, "must not delete", http.StatusInternalServerError)
		case generateAccessKeyPath:
			writeEnvelope(writer, map[string]any{"ak": "account-b-replacement", "token_id": "account-b-id"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	manager := NewManager(config.Load(), WithHTTPClient(server.Client()), withAuthBaseURLForTest(server.URL), withClockForTest(func() time.Time { return fixedNow }))
	credential, err := manager.exchangeAndProvision(context.Background(), loginGrantPayload{
		Grant: "grant", RandomSecretKey: "secret", ExpireAt: fixedNow.Add(time.Minute).Unix(),
	}, identity, LoginOptions{ForceRefresh: true})
	if err != nil || deleteCalled {
		t.Fatalf("force refresh after account switch = %#v/%v, deleteCalled=%v", credentialWithoutSecret(credential), err, deleteCalled)
	}
	if credential.UID != "account-b" || credential.TokenID != "account-b-id" {
		t.Fatalf("replacement credential = %#v", credentialWithoutSecret(credential))
	}
}

func TestGenerateAccessKeyPermissionAndLimitGuidance(t *testing.T) {
	for _, test := range []struct {
		name    string
		ret     string
		message string
	}{
		{name: "permission", ret: "3", message: "暂不具备创建 CLI Access Key 的权限"},
		{name: "possible limit", ret: "12001", message: "Access Key 数量上限"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixedNow := time.Unix(1_800_000_000, 0)
			identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{5}, deviceIDBytes)))
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case exchangeGrantPath:
					http.SetCookie(writer, &http.Cookie{Name: "session", Value: "session-value", Path: "/", Secure: true})
					writeEnvelope(writer, map[string]any{"uid": "123", "scope": loginGrantScope})
				case queryAccessKeyPath:
					writeEnvelope(writer, map[string]any{"access_token_list": []any{}})
				case generateAccessKeyPath:
					_ = json.NewEncoder(writer).Encode(map[string]any{"ret": test.ret, "errmsg": "unsafe upstream detail"})
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()
			manager := NewManager(config.Load(), WithHTTPClient(server.Client()), withAuthBaseURLForTest(server.URL), withClockForTest(func() time.Time { return fixedNow }))
			_, err := manager.exchangeAndProvision(context.Background(), loginGrantPayload{Grant: "grant", RandomSecretKey: "secret"}, identity, LoginOptions{})
			if err == nil || !strings.Contains(err.Error(), test.message) || strings.Contains(err.Error(), "unsafe upstream detail") {
				t.Fatalf("error = %v, want safe guidance containing %q", err, test.message)
			}
		})
	}
}

func TestCredentialScopeBindsUIDAndDeviceButNotAK(t *testing.T) {
	device := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, deviceIDBytes))
	first := credentialScope("account-a", device)
	if first != credentialScope("account-a", device) {
		t.Fatal("scope changed for the same UID and device")
	}
	if first == credentialScope("account-b", device) {
		t.Fatal("scope did not change across accounts")
	}
	otherDevice := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, deviceIDBytes))
	if first == credentialScope("account-a", otherDevice) {
		t.Fatal("scope did not change across devices")
	}
}

func TestSelectManagedTokenRejectsAmbiguousExactNames(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{9}, deviceIDBytes)))
	manager := NewManager(config.Load(), withClockForTest(func() time.Time { return fixedNow }))
	tokens := []accessToken{
		{ID: "one", Token: "ak-one", Name: identity.TokenName, Status: "enable", ExpiredAt: fixedNow.Add(time.Hour).Unix()},
		{ID: "two", Token: "ak-two", Name: identity.TokenName, Status: "enable", ExpiredAt: fixedNow.Add(time.Hour).Unix()},
	}
	if _, err := manager.selectManagedToken(tokens, identity); err == nil {
		t.Fatal("ambiguous exact device tokens were accepted")
	}
	identity.TokenID = "two"
	selected, err := manager.selectManagedToken(tokens, identity)
	if err != nil {
		t.Fatal(err)
	}
	if selected.ID != "two" {
		t.Fatalf("selected ID = %q", selected.ID)
	}
	selected, err = manager.selectManagedToken([]accessToken{
		{ID: "two", Token: "renamed-ak", Name: "renamed-by-user", Status: "enable", ExpiredAt: fixedNow.Add(time.Hour).Unix()},
	}, identity)
	if err != nil || selected == nil || selected.Token != "renamed-ak" {
		t.Fatalf("renamed exact TokenID was not reused: %#v/%v", selected, err)
	}
}

func TestAuthErrorsDoNotLeakRequestOrResponseSecrets(t *testing.T) {
	const (
		grant  = "grant-must-stay-secret"
		secret = "binding-must-stay-secret"
		cookie = "cookie-must-stay-secret"
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.SetCookie(writer, &http.Cookie{Name: "session", Value: cookie, Secure: true})
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(grant + secret + cookie))
	}))
	defer server.Close()
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{2}, deviceIDBytes)))
	manager := NewManager(config.Load(),
		WithCredentialStore(&memoryCredentialStore{}),
		WithHTTPClient(server.Client()),
		withAuthBaseURLForTest(server.URL),
	)
	_, err := manager.exchangeAndProvision(context.Background(), loginGrantPayload{
		Grant: grant, RandomSecretKey: secret, ExpireAt: time.Now().Add(time.Minute).Unix(),
	}, identity, LoginOptions{})
	if err == nil {
		t.Fatal("exchange error = nil")
	}
	for _, value := range []string{grant, secret, cookie} {
		if strings.Contains(err.Error(), value) {
			t.Fatalf("error leaked secret: %q", err)
		}
	}
}

func TestResilientStoreFallsBackOnlyToSecureStore(t *testing.T) {
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{4}, deviceIDBytes)))
	primary := &memoryCredentialStore{loadErr: ErrSecureStore, saveErr: ErrSecureStore, deleteErr: ErrSecureStore}
	fallback := &memoryCredentialStore{}
	store := &resilientCredentialStore{primary: primary, fallback: fallback}
	if err := store.Save(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded.DeviceID != identity.DeviceID {
		t.Fatalf("fallback load = %#v, %v", loaded, err)
	}
	if err := store.Delete(context.Background()); !errors.Is(err, ErrSecureStore) {
		t.Fatalf("delete must report unresolved primary keyring failure, got %v", err)
	}
}

func TestManagerFreshIdentityUsesEmptyFallbackWhenKeyringUnavailable(t *testing.T) {
	primary := &memoryCredentialStore{loadErr: ErrSecureStore, saveErr: ErrSecureStore}
	fallback := &memoryCredentialStore{}
	store := &resilientCredentialStore{primary: primary, fallback: fallback}
	manager := NewManager(
		config.Load(),
		WithCredentialStore(store),
		withRandomReaderForTest(bytes.NewReader(bytes.Repeat([]byte{0x2a}, deviceIDBytes))),
	)

	identity, err := manager.ensureIdentity(context.Background())
	if err != nil {
		t.Fatalf("ensureIdentity() error = %v", err)
	}
	if identity.AccessKey != "" || !validDeviceID(identity.DeviceID) {
		t.Fatalf("fresh identity = %#v", credentialWithoutSecret(identity))
	}
	fallback.mu.Lock()
	stored := cloneCredential(fallback.credential)
	fallbackLoads, fallbackSaves := fallback.loads, fallback.saves
	fallback.mu.Unlock()
	if stored == nil || stored.DeviceID != identity.DeviceID || fallbackLoads != 1 || fallbackSaves != 1 {
		t.Fatalf("fallback identity/loads/saves = %#v/%d/%d", credentialWithoutSecret(stored), fallbackLoads, fallbackSaves)
	}
	if primary.loads != 1 || primary.saves != 1 {
		t.Fatalf("primary loads/saves = %d/%d, want 1/1", primary.loads, primary.saves)
	}

	again, err := manager.ensureIdentity(context.Background())
	if err != nil || again.DeviceID != identity.DeviceID || fallback.loads != 1 {
		t.Fatalf("cached ensureIdentity() = %#v/%v, fallback loads=%d", credentialWithoutSecret(again), err, fallback.loads)
	}
}

func TestResilientStoreDoesNotMaskCorruptPrimaryOrCleanupFailure(t *testing.T) {
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{4}, deviceIDBytes)))
	corrupt := errors.New("corrupt primary credential")
	primary := &memoryCredentialStore{loadErr: corrupt}
	fallback := &memoryCredentialStore{credential: identity}
	store := &resilientCredentialStore{primary: primary, fallback: fallback}
	if _, err := store.Load(context.Background()); !errors.Is(err, corrupt) {
		t.Fatalf("corrupt primary was masked by fallback: %v", err)
	}
	if fallback.loads != 0 {
		t.Fatalf("fallback loads = %d, want zero after corrupt primary", fallback.loads)
	}

	primary = &memoryCredentialStore{}
	fallback = &memoryCredentialStore{credential: identity, deleteErr: ErrSecureStore}
	store = &resilientCredentialStore{primary: primary, fallback: fallback}
	if err := store.Save(context.Background(), identity); !errors.Is(err, ErrSecureStore) {
		t.Fatalf("stale fallback cleanup error was swallowed: %v", err)
	}
	if primary.saves != 1 || fallback.deletes != 1 {
		t.Fatalf("primary saves/fallback deletes = %d/%d", primary.saves, fallback.deletes)
	}

	primary = &memoryCredentialStore{saveErr: errors.New("invalid primary write")}
	fallback = &memoryCredentialStore{}
	store = &resilientCredentialStore{primary: primary, fallback: fallback}
	if err := store.Save(context.Background(), identity); err == nil || fallback.saves != 0 {
		t.Fatalf("non-secure-store primary failure fell back: err=%v fallback saves=%d", err, fallback.saves)
	}
}

func TestManagerCachesCredentialAndLogoutPreservesDeviceIdentity(t *testing.T) {
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

	const callers = 32
	start := make(chan struct{})
	errs := make(chan error, callers)
	var group sync.WaitGroup
	for index := 0; index < callers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			key, err := manager.ResolveAccessKey(context.Background())
			if err == nil && key != credential.AccessKey {
				err = fmt.Errorf("key = %q", key)
			}
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if store.loads != 1 {
		t.Fatalf("keyring loads = %d, want one process-local load", store.loads)
	}

	replacement := cloneCredential(credential)
	replacement.AccessKey = "replacement-ak"
	replacement.TokenID = "replacement-id"
	if err := manager.saveCredential(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	if key, err := manager.ResolveAccessKey(context.Background()); err != nil || key != replacement.AccessKey || store.loads != 1 {
		t.Fatalf("cached replacement = %q/%v, loads=%d", key, err, store.loads)
	}

	if err := manager.Logout(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	preserved := cloneCredential(store.credential)
	deletes := store.deletes
	store.mu.Unlock()
	if preserved.DeviceID != credential.DeviceID || preserved.TokenName != credential.TokenName ||
		preserved.AccessKey != "" || preserved.TokenID != replacement.TokenID || preserved.UID != "" || preserved.CredentialScope != "" {
		t.Fatalf("logout did not retain only reusable non-secret identity: %#v", credentialWithoutSecret(preserved))
	}
	if deletes != 0 {
		t.Fatalf("logout deleted the device identity %d time(s)", deletes)
	}
	if _, err := manager.ResolveAccessKey(context.Background()); !errors.Is(err, ErrCredentialNotFound) {
		t.Fatalf("post-logout resolution = %v", err)
	}
	if store.loads != 2 {
		t.Fatalf("post-logout cache was not cleared; loads=%d", store.loads)
	}
	selected, err := manager.selectManagedToken([]accessToken{{
		ID: replacement.TokenID, Token: replacement.AccessKey, Name: preserved.TokenName,
		Status: "enable", ExpiredAt: replacement.ExpiredAt,
	}}, preserved)
	if err != nil || selected == nil || selected.ID != replacement.TokenID {
		t.Fatalf("preserved identity could not reuse remote token: %#v/%v", selected, err)
	}
}

func TestManagerAuthOriginIgnoresRuntimeBaseURLAndPPE(t *testing.T) {
	cfg := config.Load()
	cfg.BaseURL = "https://untrusted.invalid"
	cfg.PPEEnv = "ppe_untrusted"
	manager := NewManager(cfg, WithCredentialStore(&memoryCredentialStore{}))
	if got := manager.authBaseURL.String(); got != config.DefaultBaseURL {
		t.Fatalf("auth base URL = %q, want %q", got, config.DefaultBaseURL)
	}
}

func TestResolveAccessKeyPrecedenceStatusAndScope(t *testing.T) {
	fixedNow := time.Unix(1_800_000_000, 0)
	identity, _ := newIdentity(bytes.NewReader(bytes.Repeat([]byte{3}, deviceIDBytes)))
	identity.AccessKey = "stored-ak"
	identity.TokenID = "stored-id"
	identity.UID = "789"
	identity.CredentialScope = credentialScope(identity.UID, identity.DeviceID)
	identity.ExpiredAt = fixedNow.Add(time.Hour).Unix()
	store := &memoryCredentialStore{credential: identity}
	cfg := config.Load()
	cfg.AccessKey = " env-ak "
	manager := NewManager(cfg, WithCredentialStore(store), withClockForTest(func() time.Time { return fixedNow }))
	accessKey, err := manager.ResolveAccessKey(context.Background())
	if err != nil || accessKey != "env-ak" {
		t.Fatalf("explicit resolution = %q, %v", accessKey, err)
	}
	status, err := manager.Status(context.Background())
	if err != nil || !status.LoggedIn || status.Source != "environment" {
		t.Fatalf("environment status = %#v, %v", status, err)
	}

	cfg.AccessKey = ""
	accessKey, err = manager.ResolveAccessKey(context.Background())
	if err != nil || accessKey != "stored-ak" {
		t.Fatalf("stored resolution = %q, %v", accessKey, err)
	}
	scope, err := manager.CredentialScope(context.Background())
	if err != nil || scope != identity.CredentialScope {
		t.Fatalf("scope = %q, %v", scope, err)
	}
	expired := cloneCredential(identity)
	expired.ExpiredAt = fixedNow.Unix()
	if err := manager.saveCredential(context.Background(), expired); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ResolveAccessKey(context.Background()); !errors.Is(err, ErrCredentialExpired) {
		t.Fatalf("expired error = %v", err)
	}
	if err := manager.Logout(context.Background(), true); !errors.Is(err, ErrRemoteRevokeUnsupported) {
		t.Fatalf("revoke error = %v", err)
	}
	if err := manager.Logout(context.Background(), false); err != nil {
		t.Fatal(err)
	}
}

func TestFileCredentialStoreIsPrivateAtomicAndNoFollow(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode = %o", info.Mode().Perm())
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
		t.Fatal("atomic save followed and overwrote symlink target")
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
	got := SanitizedBrowserEnv(input)
	joined := strings.Join(got, "\n")
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

func postCallback(t *testing.T, callbackURL, origin string, payload loginGrantPayload) int {
	t.Helper()
	status, err := sendCallback(callbackURL, origin, payload)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func sendCallback(callbackURL, origin string, payload loginGrantPayload) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequest(http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	return response.StatusCode, nil
}

func writeEnvelope(writer http.ResponseWriter, data any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"ret": "0", "data": data})
}

func assertAuthHeaders(t *testing.T, request *http.Request) {
	t.Helper()
	want := map[string]string{"appvr": "1.1.4", "entrance-from": "web", "appid": "795647"}
	for name, value := range want {
		if got := request.Header.Get(name); got != value {
			t.Errorf("header %s = %q, want %q", name, got, value)
		}
	}
}

func credentialWithoutSecret(credential *Credential) any {
	if credential == nil {
		return nil
	}
	return struct {
		DeviceID        string
		CredentialScope string
		TokenName       string
		TokenID         string
		UID             string
		ExpiredAt       int64
	}{credential.DeviceID, credential.CredentialScope, credential.TokenName, credential.TokenID, credential.UID, credential.ExpiredAt}
}
