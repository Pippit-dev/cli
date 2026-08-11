package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
)

const (
	maxAPIResponseBytes = 1 << 20
	loginGrantScope     = "xyq_novel_cli_login"
)

type apiEnvelope[T any] struct {
	Ret    json.RawMessage `json:"ret"`
	Errmsg string          `json:"errmsg"`
	Data   T               `json:"data"`
}

type apiResponseError struct {
	operation  string
	httpStatus int
	ret        string
}

func (err *apiResponseError) Error() string {
	if err.httpStatus != 0 {
		return fmt.Sprintf("%s失败（HTTP %d）", err.operation, err.httpStatus)
	}
	return fmt.Sprintf("%s失败（服务端错误码 %s）", err.operation, err.ret)
}

type exchangeData struct {
	UID   string `json:"uid"`
	Scope string `json:"scope"`
}

type queryAccessKeyData struct {
	AccessTokens []accessToken `json:"access_token_list"`
}

type accessToken struct {
	ID        string `json:"ak_id"`
	Token     string `json:"token"`
	ExpiredAt int64  `json:"expired_at"`
	Name      string `json:"token_name"`
	Status    string `json:"token_status"`
}

type generateAccessKeyData struct {
	AccessKey string `json:"ak"`
	TokenID   string `json:"token_id"`
}

func (m *Manager) exchangeAndProvision(
	ctx context.Context,
	payload loginGrantPayload,
	identity *Credential,
	options LoginOptions,
) (*Credential, error) {
	if payload.ExpireAt > 0 && payload.ExpireAt <= m.now().Unix() {
		return nil, errors.New("网页授权已过期，请重新登录")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errors.New("初始化临时网页登录会话失败")
	}
	client := *m.httpClient
	client.Jar = jar
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}

	exchangeRequest := struct {
		Grant  string `json:"grant"`
		Secret string `json:"random_secret_key"`
	}{Grant: payload.Grant, Secret: payload.RandomSecretKey}
	exchange, err := doJSON[exchangeData](ctx, &client, m.authBaseURL, exchangeGrantPath, exchangeRequest, "交换网页授权")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(exchange.UID) == "" || !constantTimeEqual(exchange.Scope, loginGrantScope) {
		return nil, errors.New("网页授权响应缺少有效的用户身份")
	}
	exchange.UID = strings.TrimSpace(exchange.UID)
	actualScope := credentialScope(exchange.UID, identity.DeviceID)
	if expected := strings.TrimSpace(options.ExpectedCredentialScope); expected != "" &&
		!constantTimeEqual(expected, actualScope) {
		return nil, ErrCredentialAccountMismatch
	}
	if len(jar.Cookies(m.authBaseURL)) == 0 {
		return nil, errors.New("网页授权响应没有建立临时登录会话")
	}

	query, err := doJSON[queryAccessKeyData](ctx, &client, m.authBaseURL, queryAccessKeyPath, nil, "查询 CLI 凭证")
	if err != nil {
		return nil, err
	}
	var rotatedTokenIDs []string
	if options.ForceRefresh {
		// A stored TokenID only identifies this device's token inside the
		// account that originally issued it. Never use it as a destructive
		// selector after the browser has switched to another UID.
		if identity.UID != "" && constantTimeEqual(identity.UID, exchange.UID) {
			rotatedTokenIDs = managedTokenIDs(query.AccessTokens, identity)
		}
		if len(rotatedTokenIDs) > 0 {
			deleteRequest := struct {
				AKIDs []string `json:"ak_ids"`
			}{AKIDs: rotatedTokenIDs}
			if _, err := doJSON[struct{}](ctx, &client, m.authBaseURL, deleteAccessKeyPath, deleteRequest, "轮换旧的 CLI 凭证"); err != nil {
				return nil, err
			}
		}
	} else {
		selected, err := m.selectManagedToken(query.AccessTokens, identity)
		if err != nil {
			return nil, err
		}
		if selected != nil {
			return credentialFromToken(identity, exchange.UID, selected), nil
		}
	}

	expiredAt := m.now().Add(DefaultCredentialLifetime).Unix()
	generateRequest := struct {
		TokenName string `json:"token_name"`
		TokenDesc string `json:"token_desc"`
		ExpiredAt int64  `json:"expired_at"`
	}{
		TokenName: identity.TokenName,
		TokenDesc: "Pippit Tool CLI browser login",
		ExpiredAt: expiredAt,
	}
	generated, err := doJSON[generateAccessKeyData](ctx, &client, m.authBaseURL, generateAccessKeyPath, generateRequest, "创建 CLI 凭证")
	if err != nil {
		var responseErr *apiResponseError
		if errors.As(err, &responseErr) && responseErr.ret == "3" {
			return nil, errors.New("当前账号暂不具备创建 CLI Access Key 的权限，请升级、联系管理员或使用已有 XYQ_ACCESS_KEY")
		}
		if errors.As(err, &responseErr) && responseErr.ret != "" {
			return nil, errors.New("无法创建新的 CLI Access Key；请在个人设置中检查 Access Key 数量上限和账号权限后重试")
		}
		return nil, err
	}
	if strings.TrimSpace(generated.AccessKey) == "" || strings.TrimSpace(generated.TokenID) == "" {
		return nil, errors.New("创建 CLI 凭证后服务端未返回完整结果")
	}
	credential := cloneCredential(identity)
	credential.AccessKey = strings.TrimSpace(generated.AccessKey)
	credential.TokenID = strings.TrimSpace(generated.TokenID)
	credential.UID = exchange.UID
	credential.CredentialScope = actualScope
	credential.ExpiredAt = expiredAt
	if options.ForceRefresh && identity.UID == credential.UID && identity.AccessKey != "" &&
		constantTimeEqual(identity.AccessKey, credential.AccessKey) {
		return nil, errors.New("服务端未轮换已失效的 CLI Access Key，已拒绝继续使用旧凭证")
	}
	for _, tokenID := range rotatedTokenIDs {
		if constantTimeEqual(tokenID, credential.TokenID) {
			return nil, errors.New("服务端未轮换已失效的 CLI 凭证编号，已拒绝继续使用旧凭证")
		}
	}
	if err := validateCredential(credential); err != nil {
		return nil, errors.New("创建的 CLI 凭证格式无效")
	}
	return credential, nil
}

func managedTokenIDs(tokens []accessToken, identity *Credential) []string {
	result := make([]string, 0, 1)
	if identity == nil || strings.TrimSpace(identity.TokenID) == "" {
		return result
	}
	for _, token := range tokens {
		id := strings.TrimSpace(token.ID)
		// QueryAk is scoped by the exchanged browser UID, while TokenID comes
		// from this device's securely stored credential. Their exact match is
		// the destructive-operation boundary even if the user renamed the token.
		if id == "" || !constantTimeEqual(id, identity.TokenID) {
			continue
		}
		result = append(result, id)
		break
	}
	return result
}

func (m *Manager) selectManagedToken(tokens []accessToken, identity *Credential) (*accessToken, error) {
	valid := make([]accessToken, 0, 1)
	minimumExpiry := m.now().Add(m.ensureTTL()).Unix()
	if identity.TokenID != "" {
		for index := range tokens {
			if constantTimeEqual(tokens[index].ID, identity.TokenID) && usableAccessToken(tokens[index], minimumExpiry) {
				// TokenID is the stable device-owned identity. Prefer it before
				// matching the display name because users may rename a token in UI.
				return &tokens[index], nil
			}
		}
	}
	for _, token := range tokens {
		if !constantTimeEqual(token.Name, identity.TokenName) || !usableAccessToken(token, minimumExpiry) {
			continue
		}
		valid = append(valid, token)
	}
	if len(valid) == 0 {
		return nil, nil
	}
	if len(valid) != 1 {
		return nil, errors.New("检测到多个同设备 CLI 凭证，拒绝自动选择；请在个人设置中清理重复项后重试")
	}
	return &valid[0], nil
}

func usableAccessToken(token accessToken, minimumExpiry int64) bool {
	return token.Status == "enable" && strings.TrimSpace(token.Token) != "" && token.ExpiredAt > minimumExpiry
}

func credentialFromToken(identity *Credential, uid string, token *accessToken) *Credential {
	credential := cloneCredential(identity)
	credential.AccessKey = strings.TrimSpace(token.Token)
	credential.TokenID = strings.TrimSpace(token.ID)
	credential.UID = strings.TrimSpace(uid)
	credential.CredentialScope = credentialScope(credential.UID, credential.DeviceID)
	credential.ExpiredAt = token.ExpiredAt
	return credential
}

func doJSON[T any](ctx context.Context, client *http.Client, baseURL *url.URL, path string, body any, operation string) (T, error) {
	var zero T
	requestURL := *baseURL
	requestURL.Path = path
	requestURL.RawPath = ""
	requestURL.RawQuery = ""
	requestURL.Fragment = ""

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return zero, redactedOperationError(operation)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), reader)
	if err != nil {
		return zero, redactedOperationError(operation)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Pippit-CLI/1.0")
	request.Header.Set("appvr", "1.1.4")
	request.Header.Set("entrance-from", "web")
	request.Header.Set("appid", "795647")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return zero, redactedOperationError(operation)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponseBytes+1))
	if err != nil || len(responseBody) > maxAPIResponseBytes {
		return zero, redactedOperationError(operation)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return zero, &apiResponseError{operation: operation, httpStatus: response.StatusCode}
	}
	envelope := apiEnvelope[T]{}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return zero, fmt.Errorf("%s响应格式无效", operation)
	}
	if !successfulRet(envelope.Ret) {
		return zero, &apiResponseError{operation: operation, ret: safeRet(envelope.Ret)}
	}
	return envelope.Data, nil
}

func successfulRet(value json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(value))
	return trimmed == "" || trimmed == "null" || trimmed == `""` || trimmed == "0" || trimmed == `"0"`
}

func safeRet(value json.RawMessage) string {
	trimmed := strings.Trim(strings.TrimSpace(string(value)), `"`)
	if _, err := strconv.ParseInt(trimmed, 10, 64); err == nil && len(trimmed) <= 20 {
		return trimmed
	}
	return "unknown"
}
