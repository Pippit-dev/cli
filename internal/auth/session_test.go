package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
)

var sessionTestNow = time.Unix(1_800_000_000, 0)

func testSessionResponse() sessionResponse {
	id := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	return sessionResponse{
		Session:         authSession{ID: id, UserCode: "ABCD-2345", Status: "pending", ExpiresAt: sessionTestNow.Add(15 * time.Minute).Unix(), Interval: 5},
		ClaimSecret:     base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
		VerificationURI: authorizePagePath + "?session_id=" + id,
	}
}

func testAuthorizedResponse(created sessionResponse) sessionResponse {
	created.ClaimSecret, created.VerificationURI = "", ""
	created.Session.Status = "authorized"
	created.Session.ClaimExpiresAt = sessionTestNow.Add(5 * time.Minute).Unix()
	created.Credential = &sessionCredential{AccessKey: "secret-session-ak", TokenID: "new-token", UID: "123", ExpiredAt: sessionTestNow.Add(time.Hour).Unix()}
	return created
}

func writeSessionResponse(t *testing.T, writer http.ResponseWriter, response sessionResponse) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(map[string]any{"ret": "0", "data": response}); err != nil {
		t.Error(err)
	}
}

func sessionManagerForTest(t *testing.T, handler http.HandlerFunc) (*Manager, *memoryCredentialStore, *[]time.Duration) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	store := &memoryCredentialStore{}
	cfg := config.Load()
	cfg.AccessKey = ""
	manager := NewManager(cfg, WithCredentialStore(store))
	manager.authBaseURL, _ = url.Parse(server.URL)
	manager.authClient = server.Client()
	now := sessionTestNow
	manager.now = func() time.Time { return now }
	delays := []time.Duration{}
	manager.wait = func(ctx context.Context, delay time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		delays = append(delays, delay)
		now = now.Add(delay)
		return nil
	}
	manager.jitter = func() time.Duration { return 0 }
	return manager, store, &delays
}

func TestSessionLoginHeadlessSavesBeforeACKAndHidesSecrets(t *testing.T) {
	created := testSessionResponse()
	var store *memoryCredentialStore
	var createRequest createSessionRequest
	var opened bool
	var acked bool
	manager, memoryStore, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.RawQuery != "" {
			t.Error("unexpected request transport")
		}
		switch r.URL.Path {
		case sessionAPIPath + "create":
			if r.Header.Get("X-Cli-Session-Secret") != "" {
				t.Error("creation included claim secret")
			}
			if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
				t.Error(err)
			}
			writeSessionResponse(t, w, created)
		case sessionAPIPath + "poll":
			if !opened || r.Header.Get("X-Cli-Session-Secret") != created.ClaimSecret {
				t.Error("unbound poll")
			}
			writeSessionResponse(t, w, testAuthorizedResponse(created))
		case sessionAPIPath + "ack":
			var request sessionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			store.mu.Lock()
			saved := cloneCredential(store.credential)
			store.mu.Unlock()
			if saved == nil || saved.AccessKey != "secret-session-ak" || request.Result != "saved" ||
				request.TokenID != saved.TokenID || request.SessionID != created.Session.ID || r.Header.Get("X-Cli-Session-Secret") != created.ClaimSecret {
				t.Error("ACK preceded save or has wrong binding")
			}
			acked = true
			response := testAuthorizedResponse(created)
			response.Session.Status, response.Credential = "completed", nil
			writeSessionResponse(t, w, response)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(http.StatusNotFound)
		}
	})
	store = memoryStore
	var progress bytes.Buffer
	credential, err := manager.Login(context.Background(), LoginOptions{Progress: &progress, OpenURL: func(value string) error {
		if !strings.Contains(progress.String(), value) || !strings.Contains(progress.String(), created.Session.UserCode) {
			t.Error("URL and device code must be shown before browser launch")
		}
		opened = true
		return errors.New("headless")
	}})
	if err != nil || credential == nil || !acked {
		t.Fatalf("login error=%v, ack=%v", err, acked)
	}
	if createRequest.DeviceID != credential.DeviceID || createRequest.Force || createRequest.ExpectedAccount != "" {
		t.Error("unexpected create binding")
	}
	if strings.Contains(progress.String(), created.ClaimSecret) || strings.Contains(progress.String(), credential.AccessKey) {
		t.Error("progress leaked credential")
	}
	resolved, err := manager.ResolveAccessKey(context.Background())
	if err != nil || resolved != credential.AccessKey {
		t.Fatalf("saved credential is unusable: %v", err)
	}
}

func TestVerificationURLRejectsUntrustedOrSecretBearingLinks(t *testing.T) {
	base, _ := url.Parse(config.DefaultBaseURL)
	created := testSessionResponse()
	for _, raw := range []string{
		"https://evil.example" + created.VerificationURI,
		"//evil.example" + created.VerificationURI,
		"http://xyq.jianying.com" + created.VerificationURI,
		"https://user@xyq.jianying.com" + created.VerificationURI,
		created.VerificationURI + "#fragment",
		created.VerificationURI + "&claim_secret=secret",
		created.VerificationURI + "&session_id=duplicate",
		created.VerificationURI + "%zz",
		"/wrong?session_id=" + created.Session.ID,
	} {
		if _, err := verificationURL(base, raw, created.Session.ID); err == nil {
			t.Errorf("accepted unsafe verification URL: %s", raw)
		}
	}
	for _, raw := range []string{created.VerificationURI, config.DefaultBaseURL + created.VerificationURI} {
		if _, err := verificationURL(base, raw, created.Session.ID); err != nil {
			t.Errorf("rejected trusted URL: %v", err)
		}
	}
}

func TestSessionRedirectNeverForwardsClaimSecret(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer target.Close()
	manager, _, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	_, err := manager.callSession(context.Background(), "poll", "never-forward-me", sessionRequest{SessionID: testSessionResponse().Session.ID})
	if err == nil || forwarded {
		t.Fatalf("redirect was followed: forwarded=%v error=%v", forwarded, err)
	}
}

type failingSaveStore struct {
	*memoryCredentialStore
	failures int
}

func (s *failingSaveStore) Save(ctx context.Context, credential *Credential) error {
	if credential.AccessKey != "" && s.failures > 0 {
		s.failures--
		return ErrSecureStore
	}
	return s.memoryCredentialStore.Save(ctx, credential)
}

func TestSessionSaveRetryAndLostACKKeepSuccessfulLogin(t *testing.T) {
	created := testSessionResponse()
	var results []string
	manager, store, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case sessionAPIPath + "create":
			writeSessionResponse(t, w, created)
		case sessionAPIPath + "poll":
			writeSessionResponse(t, w, testAuthorizedResponse(created))
		case sessionAPIPath + "ack":
			var request sessionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			results = append(results, request.Result)
			if request.Result == "saved" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeSessionResponse(t, w, created)
		}
	})
	manager.store = &failingSaveStore{memoryCredentialStore: store, failures: 1}
	credential, err := manager.Login(context.Background(), LoginOptions{OpenURL: func(string) error { return nil }})
	if err != nil || credential == nil {
		t.Fatalf("lost ACK undid successful save: %v", err)
	}
	if !reflect.DeepEqual(results, []string{"save_failed", "saved", "saved"}) {
		t.Fatalf("ACK outcomes=%v", results)
	}
	if got, err := manager.ResolveAccessKey(context.Background()); err != nil || got != credential.AccessKey {
		t.Fatalf("saved credential lost: %v", err)
	}
}

func TestSessionPollingBackoffAndServerExpiryClassification(t *testing.T) {
	created := testSessionResponse()
	polls := 0
	manager, _, delays := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sessionAPIPath+"create" {
			writeSessionResponse(t, w, created)
			return
		}
		polls++
		switch polls {
		case 1:
			_, _ = io.WriteString(w, `{"ret":"6"}`)
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
		case 3:
			_, _ = io.WriteString(w, `{"ret":"4"}`)
		case 4:
			writeSessionResponse(t, w, created)
		default:
			created.Session.Status, created.Session.Reason = "expired", "awaiting_decision"
			writeSessionResponse(t, w, created)
		}
	})
	_, err := manager.Login(context.Background(), LoginOptions{OpenURL: func(string) error { return nil }})
	if !errors.Is(err, ErrAuthorizationExpired) || !strings.Contains(err.Error(), "未在有效期内确认") {
		t.Fatalf("wrong expiry classification: %v", err)
	}
	if want := []time.Duration{5 * time.Second, 10 * time.Second, 10 * time.Second, 20 * time.Second, 10 * time.Second}; !reflect.DeepEqual(*delays, want) {
		t.Fatalf("poll delays=%v, want=%v", *delays, want)
	}
}

func TestSessionDoesNotDowngradeUnsupportedServer(t *testing.T) {
	opened := false
	manager, _, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	_, err := manager.Login(context.Background(), LoginOptions{OpenURL: func(string) error { opened = true; return nil }})
	if !errors.Is(err, ErrSessionUnsupported) || opened {
		t.Fatalf("unexpected fallback: opened=%v err=%v", opened, err)
	}
}

func TestSessionPreservesBindingAndRotationValidation(t *testing.T) {
	deviceID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	identity := &Credential{Version: credentialVersion, DeviceID: deviceID, AccessKey: "old-key", TokenID: "old-token", UID: "123"}
	request := createSessionRequest{DeviceID: deviceID, TokenID: "old-token", ExpectedAccount: accountBinding("123"), Force: true}
	manager := NewManager(config.Load(), withClockForTest(func() time.Time { return sessionTestNow }))
	for _, test := range []struct {
		name   string
		modify func(*sessionCredential)
	}{
		{"account", func(c *sessionCredential) { c.UID = "456" }},
		{"team", func(c *sessionCredential) { c.TeamID = "999" }},
		{"old token", func(c *sessionCredential) { c.TokenID = "old-token" }},
		{"old key", func(c *sessionCredential) { c.AccessKey = "old-key" }},
		{"expired", func(c *sessionCredential) { c.ExpiredAt = sessionTestNow.Unix() }},
		{"missing key", func(c *sessionCredential) { c.AccessKey = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := testAuthorizedResponse(testSessionResponse()).Credential
			test.modify(payload)
			if _, err := manager.validateSessionCredential(identity, request, LoginOptions{}, payload); err == nil {
				t.Error("accepted invalid credential")
			}
		})
	}
	if _, err := manager.validateSessionCredential(identity, request, LoginOptions{ExpectedCredentialScope: credentialScope("123", deviceID)}, testAuthorizedResponse(testSessionResponse()).Credential); err != nil {
		t.Fatalf("valid rotation rejected: %v", err)
	}
}

func TestSessionTotalWaitTimeoutIsDistinctFromRequestTimeout(t *testing.T) {
	manager, _, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) { writeSessionResponse(t, w, testSessionResponse()) })
	manager.wait = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	_, err := manager.Login(context.Background(), LoginOptions{Timeout: time.Second, OpenURL: func(string) error { return nil }})
	if !errors.Is(err, ErrLoginWaitTimeout) || errors.Is(err, ErrAuthorizationExpired) {
		t.Fatalf("wrong timeout: %v", err)
	}
}

func TestSessionValidationRejectsBindingChanges(t *testing.T) {
	created := testSessionResponse()
	for _, mutate := range []func(*authSession){
		func(s *authSession) { s.ID = "invalid" },
		func(s *authSession) { s.ExpectedAccount = accountBinding("other") },
		func(s *authSession) { s.Force = true },
		func(s *authSession) { s.TeamID = "888" },
		func(s *authSession) { s.Status = "unknown" },
		func(s *authSession) { s.UserCode = "bad\ncode" },
		func(s *authSession) { s.Interval = 1 << 62 },
	} {
		session := created.Session
		mutate(&session)
		if validateSession(session, createSessionRequest{}, created.Session.ID) == nil {
			t.Error("accepted invalid session")
		}
	}
}

func TestSessionCanClaimApprovalAtAuthorizationDeadline(t *testing.T) {
	created := testSessionResponse()
	created.Session.ExpiresAt = sessionTestNow.Add(6 * time.Second).Unix()
	polls := 0
	manager, _, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sessionAPIPath+"poll" {
			polls++
			if polls > 1 {
				response := testAuthorizedResponse(created)
				response.Session.ClaimExpiresAt = created.Session.ExpiresAt + int64(claimWindow/time.Second)
				writeSessionResponse(t, w, response)
				return
			}
		}
		writeSessionResponse(t, w, created)
	})
	credential, err := manager.Login(context.Background(), LoginOptions{OpenURL: func(string) error { return nil }})
	if err != nil || credential == nil || polls != 2 {
		t.Fatalf("deadline approval lost: polls=%d err=%v", polls, err)
	}
}

func TestSessionAccountMismatchDoesNotOverwriteOrACK(t *testing.T) {
	created := testSessionResponse()
	created.Session.ExpectedAccount = accountBinding("original-user")
	acked := false
	manager, store, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case sessionAPIPath + "create":
			writeSessionResponse(t, w, created)
		case sessionAPIPath + "poll":
			writeSessionResponse(t, w, testAuthorizedResponse(created))
		case sessionAPIPath + "ack":
			acked = true
			writeSessionResponse(t, w, created)
		}
	})
	deviceID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	store.credential = &Credential{Version: credentialVersion, DeviceID: deviceID, UID: "original-user", AccessKey: "original-ak", TokenID: "original-token", ExpiredAt: sessionTestNow.Add(time.Hour).Unix()}
	_, err := manager.Login(context.Background(), LoginOptions{OpenURL: func(string) error { return nil }})
	if !errors.Is(err, ErrCredentialAccountMismatch) || acked {
		t.Fatalf("invalid account accepted/ACKed: %v", err)
	}
	if store.credential.AccessKey != "original-ak" || store.saves != 0 {
		t.Fatal("existing credential was overwritten")
	}
}

func TestSessionTransportTimeoutDoesNotExposeUnderlyingError(t *testing.T) {
	err := sessionTransportError(context.Background(), &url.Error{Op: "Post", URL: "https://example.invalid/secret", Err: context.DeadlineExceeded})
	var requestErr *sessionRequestError
	if !errors.As(err, &requestErr) || !requestErr.timeout || !requestErr.retryable || strings.Contains(err.Error(), "secret") {
		t.Fatalf("incorrect timeout classification: %v", err)
	}
	if errors.Is(err, ErrLoginWaitTimeout) {
		t.Fatal("individual request timeout became total login timeout")
	}
	if got := retryAfterDelay("120", sessionTestNow); got != 2*time.Minute {
		t.Fatalf("Retry-After=%v", got)
	}
}

func TestSessionLoginAfterLogoutUsesDeviceWithoutStaleAccountSelector(t *testing.T) {
	created := testSessionResponse()
	var request createSessionRequest
	manager, store, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sessionAPIPath+"create" {
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
		}
		if r.URL.Path == sessionAPIPath+"poll" {
			writeSessionResponse(t, w, testAuthorizedResponse(created))
			return
		}
		writeSessionResponse(t, w, created)
	})
	deviceID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	store.credential = &Credential{Version: credentialVersion, DeviceID: deviceID, TokenID: "old-account-token"}
	credential, err := manager.Login(context.Background(), LoginOptions{OpenURL: func(string) error { return nil }})
	if err != nil || credential == nil {
		t.Fatalf("login after logout failed: %v", err)
	}
	if request.DeviceID != deviceID || request.TokenID != "" || request.ExpectedAccount != "" || request.Force {
		t.Error("stale account selector sent after logout")
	}
}

func TestSessionHTTPTimeoutRemainsRequestTimeout(t *testing.T) {
	for _, status := range []int{http.StatusGatewayTimeout, http.StatusRequestTimeout} {
		manager, _, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
		_, err := manager.callSession(context.Background(), "create", "", createSessionRequest{})
		var requestErr *sessionRequestError
		if !errors.As(err, &requestErr) || !requestErr.timeout || !requestErr.retryable || errors.Is(err, ErrLoginWaitTimeout) {
			t.Fatalf("HTTP %d incorrectly classified: %v", status, err)
		}
	}
}

func TestSessionUncertainAuthorizationReconcilesWithoutRestartOrRotation(t *testing.T) {
	created := testSessionResponse()
	creates, polls, acks := 0, 0, 0
	manager, _, delays := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case sessionAPIPath + "create":
			creates++
			var request createSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Force {
				t.Error("unexpected automatic key rotation")
			}
			writeSessionResponse(t, w, created)
		case sessionAPIPath + "poll":
			polls++
			var request sessionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.SessionID != created.Session.ID || r.Header.Get("X-Cli-Session-Secret") != created.ClaimSecret {
				t.Error("poll switched the authorization session")
			}
			switch polls {
			case 1, 4:
				response := created
				response.Session.Reason = "authorization_uncertain"
				writeSessionResponse(t, w, response)
			case 2:
				_, _ = io.WriteString(w, `{"ret":"6"}`)
			case 3:
				_, _ = io.WriteString(w, `{"ret":"20002"}`)
			default:
				writeSessionResponse(t, w, testAuthorizedResponse(created))
			}
		case sessionAPIPath + "ack":
			acks++
			writeSessionResponse(t, w, created)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(http.StatusNotFound)
		}
	})
	var progress bytes.Buffer
	credential, err := manager.Login(context.Background(), LoginOptions{Progress: &progress, OpenURL: func(string) error { return nil }})
	if err != nil || credential == nil || creates != 1 || polls != 5 || acks != 1 {
		t.Fatalf("uncertain authorization was not reconciled: creates=%d polls=%d acks=%d err=%v", creates, polls, acks, err)
	}
	if want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 5 * time.Second}; !reflect.DeepEqual(*delays, want) {
		t.Fatalf("poll delays=%v, want=%v", *delays, want)
	}
	if strings.Count(progress.String(), "授权结果尚未确认") != 1 || !strings.Contains(progress.String(), "请勿重复发起授权或更换密钥") {
		t.Error("uncertainty must be explained once while continuing the same session")
	}
	if strings.Contains(progress.String(), created.ClaimSecret) || strings.Contains(progress.String(), credential.AccessKey) {
		t.Error("progress leaked credential")
	}
}

func TestSessionUncertainAuthorizationExpiryPreservesCredentialsAndClassification(t *testing.T) {
	for _, test := range []struct {
		name    string
		waitErr error
	}{
		{name: "server expiry"},
		{name: "local deadline", waitErr: context.DeadlineExceeded},
		{name: "server wait deadline", waitErr: ErrLoginWaitTimeout},
		{name: "cancelled wait", waitErr: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			created := testSessionResponse()
			created.Session.ExpectedAccount = accountBinding("123")
			creates, polls, acks := 0, 0, 0
			manager, store, _ := sessionManagerForTest(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case sessionAPIPath + "create":
					creates++
					writeSessionResponse(t, w, created)
				case sessionAPIPath + "poll":
					polls++
					response := created
					response.Session.Reason = "authorization_uncertain"
					if polls > 1 {
						response.Session.Status = "expired"
					}
					writeSessionResponse(t, w, response)
				case sessionAPIPath + "ack":
					acks++
					writeSessionResponse(t, w, created)
				default:
					t.Error("unexpected endpoint")
					w.WriteHeader(http.StatusNotFound)
				}
			})
			deviceID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
			original := &Credential{Version: credentialVersion, DeviceID: deviceID, UID: "123", AccessKey: "original-ak", TokenID: "original-token", ExpiredAt: sessionTestNow.Add(time.Hour).Unix()}
			store.credential = cloneCredential(original)
			if test.waitErr != nil {
				wait := manager.wait
				manager.wait = func(ctx context.Context, delay time.Duration) error {
					if polls > 0 {
						return test.waitErr
					}
					return wait(ctx, delay)
				}
			}
			_, err := manager.Login(context.Background(), LoginOptions{OpenURL: func(string) error { return nil }})
			if !errors.Is(err, ErrAuthorizationUncertain) || errors.Is(err, ErrAuthorizationExpired) {
				t.Fatalf("uncertainty lost at expiry: %v", err)
			}
			if test.waitErr != nil && !errors.Is(err, test.waitErr) {
				t.Fatalf("underlying stop reason lost: %v", err)
			}
			if strings.Contains(err.Error(), "请重新登录") || !strings.Contains(err.Error(), "不要直接重复授权或换钥") {
				t.Fatalf("unsafe retry advice: %v", err)
			}
			if creates != 1 || acks != 0 || store.saves != 0 || !reflect.DeepEqual(store.credential, original) {
				t.Fatalf("uncertain result restarted, ACKed or replaced credentials: creates=%d acks=%d saves=%d", creates, acks, store.saves)
			}
		})
	}
}
