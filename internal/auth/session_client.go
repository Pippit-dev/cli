package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	sessionAPIPath          = "/api/web/v1/cli/auth/session/"
	authorizePagePath       = "/cli/pippit-tool-authorize"
	minimumPollInterval     = 5 * time.Second
	claimWindow             = 5 * time.Minute
	maxSessionResponseBytes = 64 << 10
)

type authSession struct {
	ID              string `json:"session_id"`
	UserCode        string `json:"user_code"`
	Status          string `json:"status"`
	Reason          string `json:"reason"`
	ExpiresAt       int64  `json:"expires_at"`
	ClaimExpiresAt  int64  `json:"claim_expires_at"`
	Interval        int64  `json:"interval"`
	ExpectedAccount string `json:"expected_account"`
	Force           bool   `json:"force"`
	TeamID          string `json:"team_id"`
}

type sessionCredential struct {
	AccessKey string `json:"ak"`
	TokenID   string `json:"token_id"`
	UID       string `json:"uid"`
	ExpiredAt int64  `json:"expired_at"`
	TeamID    string `json:"team_id"`
}

type sessionResponse struct {
	Session         authSession        `json:"session"`
	ClaimSecret     string             `json:"claim_secret"`
	VerificationURI string             `json:"verification_uri"`
	Credential      *sessionCredential `json:"credential"`
}

type createSessionRequest struct {
	DeviceID        string `json:"device_id"`
	ExpectedAccount string `json:"expected_account,omitempty"`
	TokenID         string `json:"token_id,omitempty"`
	Force           bool   `json:"force,omitempty"`
}

type sessionRequest struct {
	SessionID string `json:"session_id"`
	TokenID   string `json:"token_id,omitempty"`
	Result    string `json:"result,omitempty"`
}

type sessionRequestError struct {
	retryable  bool
	slowDown   bool
	timeout    bool
	retryAfter time.Duration
}

func (e *sessionRequestError) Error() string {
	if e.timeout {
		return "授权服务请求超时"
	}
	if e.slowDown {
		return "授权请求过于频繁，请稍后重试"
	}
	if e.retryable {
		return "授权服务暂不可用，请稍后重试"
	}
	return "授权请求失败，请重新登录"
}

// The client uses the canonical authentication origin, independently of the
// business API base URL. No redirect may carry a claim secret to another URL.
func (m *Manager) callSession(ctx context.Context, action, secret string, input any) (*sessionResponse, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, errors.New("授权请求格式无效")
	}
	endpoint := *m.authBaseURL
	endpoint.Path, endpoint.RawPath = sessionAPIPath+action, ""
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("授权请求地址无效")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if secret != "" {
		request.Header.Set("X-Cli-Session-Secret", secret)
	}
	client := *m.authClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil
	response, err := client.Do(request)
	if err != nil {
		return nil, sessionTransportError(ctx, err)
	}
	defer response.Body.Close()
	if action == "create" && (response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed) {
		return nil, ErrSessionUnsupported
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		timedOut := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusGatewayTimeout
		return nil, &sessionRequestError{
			retryable:  timedOut || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500,
			slowDown:   response.StatusCode == http.StatusTooManyRequests,
			timeout:    timedOut,
			retryAfter: retryAfterDelay(response.Header.Get("Retry-After"), m.now()),
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSessionResponseBytes+1))
	if err != nil {
		return nil, sessionTransportError(ctx, err)
	}
	if len(body) > maxSessionResponseBytes {
		return nil, errors.New("授权服务返回内容过大")
	}
	var envelope struct {
		Ret  string           `json:"ret"`
		Data *sessionResponse `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return nil, errors.New("授权服务响应格式无效")
	}
	// Values come from capcut_business_common_code.thrift. Never classify
	// errors by untrusted errmsg text or copy it into terminal output.
	if envelope.Ret != "0" {
		return nil, &sessionRequestError{
			retryable: envelope.Ret == "4" || envelope.Ret == "6" || envelope.Ret == "10" || envelope.Ret == "20002",
			slowDown:  envelope.Ret == "10", timeout: envelope.Ret == "6",
			retryAfter: retryAfterDelay(response.Header.Get("Retry-After"), m.now()),
		}
	}
	if envelope.Data == nil {
		return nil, errors.New("授权服务响应缺少会话")
	}
	return envelope.Data, nil
}

func sessionTransportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var timeout interface{ Timeout() bool }
	return &sessionRequestError{retryable: true, timeout: errors.As(err, &timeout) && timeout.Timeout()}
}

func retryAfterDelay(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return time.Duration(min(seconds, 3600)) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) {
		return min(deadline.Sub(now), time.Hour)
	}
	return 0
}

func validSessionSecret(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32 && len(value) == 43 &&
		constantTimeEqual(value, base64.RawURLEncoding.EncodeToString(decoded))
}

func personalSpace(teamID string) bool { return teamID == "" || teamID == "0" }

func validateSession(session authSession, request createSessionRequest, sessionID string) error {
	if !validSessionSecret(session.ID) || (sessionID != "" && !constantTimeEqual(sessionID, session.ID)) ||
		!personalSpace(session.TeamID) || session.Force != request.Force ||
		!constantTimeEqual(session.ExpectedAccount, request.ExpectedAccount) ||
		session.ExpiresAt <= 0 || session.Interval < 0 || session.Interval > 3600 {
		return errors.New("授权会话绑定无效")
	}
	if len(session.UserCode) != 9 || session.UserCode[4] != '-' {
		return errors.New("授权设备码无效")
	}
	for i, c := range session.UserCode {
		if i != 4 && !strings.ContainsRune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789", c) {
			return errors.New("授权设备码无效")
		}
	}
	switch session.Status {
	case "pending", "denied", "expired":
	case "authorized", "completed":
		if session.ClaimExpiresAt <= 0 || session.ClaimExpiresAt > session.ExpiresAt+int64(claimWindow/time.Second) {
			return errors.New("授权领取期限无效")
		}
	default:
		return errors.New("授权会话状态无效")
	}
	return nil
}

func verificationURL(base *url.URL, rawURI, sessionID string) (string, error) {
	reference, err := url.Parse(rawURI)
	if err != nil {
		return "", errors.New("网页授权地址无效")
	}
	target := base.ResolveReference(reference)
	query, queryErr := url.ParseQuery(target.RawQuery)
	if queryErr != nil || target.User != nil || target.Opaque != "" || target.Fragment != "" ||
		target.Scheme != base.Scheme || !strings.EqualFold(target.Host, base.Host) ||
		target.Path != authorizePagePath || target.RawPath != "" || len(query) != 1 ||
		len(query["session_id"]) != 1 || !constantTimeEqual(query.Get("session_id"), sessionID) {
		return "", errors.New("网页授权地址不可信")
	}
	return target.String(), nil
}
