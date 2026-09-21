package auth

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrAuthorizationUncertain means a key mutation was attempted but its result
// has not been confirmed. Starting another login may repeat that mutation.
var ErrAuthorizationUncertain = errors.New("本次授权结果尚未确认，密钥可能已创建或更换；请先核对终端和账号中的密钥状态，不要直接重复授权或换钥")

type authorizationUncertainError struct{ cause error }

func (e *authorizationUncertainError) Error() string { return ErrAuthorizationUncertain.Error() }
func (e *authorizationUncertainError) Unwrap() error { return e.cause }
func (e *authorizationUncertainError) Is(target error) bool {
	return target == ErrAuthorizationUncertain
}

func (m *Manager) loginSession(ctx context.Context, options LoginOptions) (*Credential, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	if m.authClient == nil || m.wait == nil || m.jitter == nil {
		return nil, errors.New("授权客户端未正确初始化")
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultSessionLoginTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	credential, err := m.runSessionLogin(waitCtx, options)
	if errors.Is(err, ErrAuthorizationUncertain) {
		return credential, err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, ErrLoginWaitTimeout
	}
	return credential, err
}

func (m *Manager) runSessionLogin(ctx context.Context, options LoginOptions) (*Credential, error) {
	identity, err := m.ensureIdentity(ctx)
	if err != nil {
		return nil, err
	}
	request := createSessionRequest{DeviceID: identity.DeviceID, TokenID: identity.TokenID,
		Force: options.ForceRefresh && strings.TrimSpace(identity.TokenID) != ""}
	if identity.UID != "" {
		request.ExpectedAccount = accountBinding(identity.UID)
	}
	if expected := strings.TrimSpace(options.ExpectedCredentialScope); expected != "" {
		binding, ok := accountBindingFromCredentialScope(expected, identity.DeviceID)
		if !ok || (request.ExpectedAccount != "" && !constantTimeEqual(request.ExpectedAccount, binding)) {
			return nil, ErrCredentialAccountMismatch
		}
		request.ExpectedAccount = binding
	}
	// Logout preserves the device and a non-secret token selector but clears
	// the account. Without an account binding, ordinary login must let the
	// confirmed user's device name select the token instead of sending a stale
	// selector from another account. Force still requires the original binding.
	if request.ExpectedAccount == "" && !request.Force {
		request.TokenID = ""
	}
	if !validDeviceID(request.DeviceID) || (request.TokenID != "" && !validTokenID(request.TokenID)) ||
		(request.ExpectedAccount != "" && !validAccountBinding(request.ExpectedAccount)) ||
		(request.Force && request.ExpectedAccount == "") {
		return nil, errors.New("本机登录设备或账号绑定无效")
	}
	// Creation is not automatically retried: a lost response must not create
	// multiple pending sessions for one login. The user can start a fresh login.
	created, err := m.callSession(ctx, "create", "", request)
	if err != nil {
		return nil, err
	}
	if err := validateSession(created.Session, request, ""); err != nil {
		return nil, err
	}
	if created.Session.Status != "pending" || !validSessionSecret(created.ClaimSecret) || created.Credential != nil ||
		created.Session.ExpiresAt <= m.now().Unix() || created.Session.ExpiresAt > m.now().Add(15*time.Minute+time.Minute).Unix() {
		return nil, errors.New("新建授权会话无效")
	}
	loginURL, err := verificationURL(m.authBaseURL, created.VerificationURI, created.Session.ID)
	if err != nil {
		return nil, err
	}
	writeProgress(options.Progress, "小云雀网页授权地址（如未自动打开，可复制到任意设备的浏览器）：")
	writeProgress(options.Progress, loginURL)
	writeProgress(options.Progress, "请核对网页上的设备码："+created.Session.UserCode)
	opener := options.OpenURL
	if opener == nil {
		opener = OpenBrowser
	}
	if opener(loginURL) != nil {
		writeProgress(options.Progress, "未能自动打开浏览器，请复制上方地址继续；CLI 将继续等待授权。")
	}
	writeProgress(options.Progress, "请在浏览器中登录个人空间并确认授权，CLI 会自动继续…")
	// A pending response near the authorization deadline may race with approval.
	// Keep polling through the possible claim window; only the server can report
	// whether authorization expired or was approved just before its deadline.
	serverDeadline := time.Unix(created.Session.ExpiresAt, 0).Add(claimWindow)
	interval := max(minimumPollInterval, time.Duration(created.Session.Interval)*time.Second)
	delay := interval
	authorizationUncertain := false
	for {
		if err := m.waitSession(ctx, delay, serverDeadline); err != nil {
			if authorizationUncertain {
				return nil, &authorizationUncertainError{cause: err}
			}
			return nil, err
		}
		result, pollErr := m.callSession(ctx, "poll", created.ClaimSecret, sessionRequest{SessionID: created.Session.ID})
		if pollErr != nil {
			var requestErr *sessionRequestError
			if !errors.As(pollErr, &requestErr) || !requestErr.retryable {
				if authorizationUncertain {
					return nil, &authorizationUncertainError{cause: pollErr}
				}
				return nil, pollErr
			}
			delay, interval = nextPollDelay(delay, interval, requestErr)
			continue
		}
		if err := validateSession(result.Session, request, created.Session.ID); err != nil {
			return nil, err
		}
		if result.Session.ExpiresAt != created.Session.ExpiresAt || result.Session.UserCode != created.Session.UserCode {
			return nil, errors.New("授权会话在等待中发生变化")
		}
		interval = max(interval, time.Duration(result.Session.Interval)*time.Second)
		delay = interval
		switch result.Session.Status {
		case "pending":
			if result.Credential != nil {
				return nil, errors.New("未确认授权返回了凭据")
			}
			if result.Session.Reason == "authorization_uncertain" && !authorizationUncertain {
				authorizationUncertain = true
				writeProgress(options.Progress, "授权结果尚未确认，CLI 正在继续查询当前会话；请勿重复发起授权或更换密钥。")
			}
			continue
		case "denied":
			if result.Session.Reason == "policy_denied" {
				return nil, ErrAuthorizationPolicy
			}
			return nil, ErrAuthorizationDenied
		case "expired":
			return nil, sessionExpiredError(result.Session.Reason)
		case "authorized", "completed":
			if result.Session.ClaimExpiresAt <= m.now().Unix() {
				return nil, sessionExpiredError("delivery_unconfirmed")
			}
			credential, err := m.validateSessionCredential(identity, request, options, result.Credential)
			if err != nil {
				return nil, err
			}
			writeProgress(options.Progress, "网页授权已完成，正在安全保存本机 CLI 凭证…")
			if err := m.saveSessionCredential(ctx, created, result.Session, credential); err != nil {
				return nil, err
			}
			writeProgress(options.Progress, "小云雀 CLI 登录成功。")
			return cloneCredential(credential), nil
		}
	}
}

func (m *Manager) validateSessionCredential(identity *Credential, request createSessionRequest, options LoginOptions, payload *sessionCredential) (*Credential, error) {
	if payload == nil || payload.AccessKey == "" || !personalSpace(payload.TeamID) {
		return nil, errors.New("网页返回的 CLI 凭据无效或不属于个人空间")
	}
	credential := credentialFromCallback(identity, accessKeyPayload{AccessKey: payload.AccessKey,
		TokenID: payload.TokenID, UID: payload.UID, ExpiredAt: payload.ExpiredAt})
	if err := validateCredential(credential); err != nil {
		return nil, errors.New("网页返回的 CLI 凭证格式无效")
	}
	if credential.ExpiredAt <= m.now().Add(m.ensureTTL()).Unix() {
		return nil, ErrCredentialExpired
	}
	if request.ExpectedAccount != "" && !constantTimeEqual(request.ExpectedAccount, accountBinding(credential.UID)) {
		return nil, ErrCredentialAccountMismatch
	}
	if expected := strings.TrimSpace(options.ExpectedCredentialScope); expected != "" && !constantTimeEqual(expected, credential.CredentialScope) {
		return nil, ErrCredentialAccountMismatch
	}
	if request.Force && ((identity.AccessKey != "" && constantTimeEqual(identity.AccessKey, credential.AccessKey)) ||
		constantTimeEqual(identity.TokenID, credential.TokenID)) {
		return nil, errors.New("网页授权未轮换已失效的 CLI Access Key，已拒绝继续使用旧凭证")
	}
	return credential, nil
}

func (m *Manager) saveSessionCredential(ctx context.Context, created *sessionResponse, session authSession, credential *Credential) error {
	deadline := time.Unix(session.ClaimExpiresAt, 0)
	delay := time.Second
	reportedFailure := false
	for {
		if credential.ExpiredAt <= m.now().Add(m.ensureTTL()).Unix() {
			return ErrCredentialExpired
		}
		err := m.saveCredential(ctx, credential)
		if err == nil {
			// Save is the commit point. ACK has a separate, short retry budget and
			// must never undo a successful save, even if the caller is cancelled.
			m.ackSession(ctx, created, credential.TokenID, "saved")
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !reportedFailure {
			m.ackSession(ctx, created, credential.TokenID, "save_failed")
			reportedFailure = true
		}
		if waitErr := m.waitSession(ctx, delay, deadline); waitErr != nil {
			return fmt.Errorf("保存 CLI 凭据失败，授权领取窗口或等待时间已结束: %w", err)
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (m *Manager) ackSession(ctx context.Context, created *sessionResponse, tokenID, result string) {
	ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		_, err := m.callSession(ackCtx, "ack", created.ClaimSecret, sessionRequest{
			SessionID: created.Session.ID, TokenID: tokenID, Result: result,
		})
		if err == nil {
			return
		}
		var requestErr *sessionRequestError
		if !errors.As(err, &requestErr) || !requestErr.retryable {
			return
		}
		// A limiter response is not retried within this short ACK budget.
		if requestErr.slowDown || requestErr.retryAfter > 0 {
			return
		}
		if attempt == 0 && m.wait(ackCtx, 250*time.Millisecond) != nil {
			return
		}
	}
}

func (m *Manager) waitSession(ctx context.Context, delay time.Duration, deadline time.Time) error {
	remaining := deadline.Sub(m.now())
	if remaining <= 0 {
		return ErrLoginWaitTimeout
	}
	delay += max(0, m.jitter())
	if delay >= remaining {
		if err := m.wait(ctx, remaining); err != nil {
			return err
		}
		return ErrLoginWaitTimeout
	}
	return m.wait(ctx, delay)
}

func nextPollDelay(delay, interval time.Duration, requestErr *sessionRequestError) (time.Duration, time.Duration) {
	if requestErr.slowDown {
		interval = max(interval, minimumPollInterval) + minimumPollInterval
		delay = max(delay, interval)
	} else {
		delay = max(interval, min(delay*2, time.Minute))
	}
	return max(delay, requestErr.retryAfter), interval
}

func sessionExpiredError(reason string) error {
	switch reason {
	case "authorization_uncertain":
		return ErrAuthorizationUncertain
	case "no_observed_action":
		return fmt.Errorf("%w（服务端未观察到打开授权页面）", ErrAuthorizationExpired)
	case "awaiting_decision":
		return fmt.Errorf("%w（未在有效期内确认）", ErrAuthorizationExpired)
	case "delivery_unconfirmed":
		return fmt.Errorf("%w（凭据领取或保存结果未确认）", ErrAuthorizationExpired)
	case "client_save_failed":
		return fmt.Errorf("%w（CLI 曾报告本机保存失败）", ErrAuthorizationExpired)
	default:
		return ErrAuthorizationExpired
	}
}

func waitForAuthorization(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func authorizationJitter() time.Duration {
	var value [2]byte
	if _, err := rand.Read(value[:]); err != nil {
		return 0
	}
	return time.Duration(binary.BigEndian.Uint16(value[:])%1000) * time.Millisecond
}
