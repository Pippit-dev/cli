package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/config"
	"github.com/bytedance/sonic"
)

const (
	ppeUseHeader         = "x-use-ppe"
	ppeEnvHeader         = "x-tt-env"
	ppeScheduleVDCHeader = "x-schedule-vdc"
	defaultPPEVDC        = "sinfonlinea"
)

type Client interface {
	SendRequest(ctx context.Context, path string, body any, out any) error
	SendRequestWithHeaders(ctx context.Context, path string, body any, headers map[string]string, out any) error
	SendMultipartRequest(ctx context.Context, path string, fields map[string]string, file MultipartFile, out any) error
}

type RequestAuthorizer interface {
	Inject(ctx context.Context, req *http.Request) error
}

type MultipartFile struct {
	FieldName   string
	Path        string
	FileName    string
	ContentType string
	Reader      io.Reader
}

type httpClient struct {
	baseURL    string
	httpClient *http.Client
	headers    http.Header
	authorizer RequestAuthorizer
	ppeEnv     func() string
}

func NewHTTPClient(baseURL string, timeout time.Duration, authorizer RequestAuthorizer) Client {
	return newHTTPClient(baseURL, timeout, authorizer, nil)
}

// NewHTTPClientWithPPEEnv creates a client whose PPE lane is resolved for each
// request. The provider allows Cobra's global --ppe-env flag to override the
// environment after command construction but before the request is sent.
func NewHTTPClientWithPPEEnv(baseURL string, timeout time.Duration, authorizer RequestAuthorizer, ppeEnv func() string) Client {
	return newHTTPClient(baseURL, timeout, authorizer, ppeEnv)
}

func newHTTPClient(baseURL string, timeout time.Duration, authorizer RequestAuthorizer, ppeEnv func() string) Client {
	client := &httpClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		headers:    make(http.Header),
		authorizer: authorizer,
		ppeEnv:     ppeEnv,
	}
	client.httpClient = &http.Client{
		Timeout:       timeout,
		CheckRedirect: client.checkRedirect,
	}
	return client
}

func (c *httpClient) SendRequest(ctx context.Context, path string, body any, out any) error {
	return c.SendRequestWithHeaders(ctx, path, body, nil, out)
}

func (c *httpClient) SendRequestWithHeaders(ctx context.Context, path string, body any, headers map[string]string, out any) error {
	method := http.MethodPost
	if body == nil {
		method = http.MethodGet
	}

	reqURL, err := c.resolveURL(path, nil)
	if err != nil {
		return err
	}

	var reader io.Reader
	if body != nil {
		payload, err := sonic.Marshal(body)
		if err != nil {
			return fmt.Errorf("编码请求体失败: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return fmt.Errorf("构造 %s 请求失败: %w", method, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if err := c.prepareRequest(ctx, req, headers); err != nil {
		return err
	}

	// If out is **http.Response, return the raw response for streaming (e.g. file download).
	if out != nil {
		if rv := reflect.ValueOf(out); rv.Kind() == reflect.Ptr && rv.Elem().Kind() == reflect.Ptr {
			if rv.Elem().Type().Elem() == reflect.TypeOf(http.Response{}) {
				resp, err := c.httpClient.Do(req)
				if err != nil {
					return fmt.Errorf("%s %s 请求失败: %w", method, reqURL, err)
				}
				if resp.StatusCode >= 400 {
					defer resp.Body.Close()
					return fmt.Errorf("%s %s 返回 HTTP %d", method, reqURL, resp.StatusCode)
				}
				rv.Elem().Set(reflect.ValueOf(resp))
				return nil
			}
		}
	}

	req.Header.Set("Accept", "application/json")

	return c.do(req, out)
}

func (c *httpClient) SendMultipartRequest(ctx context.Context, path string, fields map[string]string, file MultipartFile, out any) error {
	reqURL, err := c.resolveURL(path, nil)
	if err != nil {
		return err
	}
	if file.FieldName == "" {
		return fmt.Errorf("multipart 文件字段名不能为空")
	}
	if file.FileName == "" {
		file.FileName = filepath.Base(file.Path)
	}
	if strings.TrimSpace(file.FileName) == "" || file.FileName == "." {
		return fmt.Errorf("multipart 文件名不能为空")
	}
	if file.ContentType == "" {
		file.ContentType = "application/octet-stream"
	}

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, pr)
	if err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return fmt.Errorf("构造 POST 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	if err := c.prepareRequest(ctx, req, nil); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return err
	}

	go func() {
		err := writeMultipartBody(writer, fields, file)
		closeErr := writer.Close()
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.CloseWithError(closeErr)
	}()

	return c.do(req, out)
}

func writeMultipartBody(writer *multipart.Writer, fields map[string]string, file MultipartFile) error {
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return err
		}
	}

	reader := file.Reader
	var opened *os.File
	if reader == nil {
		var err error
		opened, err = os.Open(file.Path)
		if err != nil {
			return fmt.Errorf("打开上传文件失败: %w", err)
		}
		defer opened.Close()
		reader = opened
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(file.FieldName), escapeQuotes(file.FileName)))
	header.Set("Content-Type", file.ContentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, reader); err != nil {
		return fmt.Errorf("写入上传文件失败: %w", err)
	}
	return nil
}

var quotesReplacer = strings.NewReplacer("\\", "\\\\", `"`, `\"`)

func escapeQuotes(s string) string {
	return quotesReplacer.Replace(s)
}

func (c *httpClient) injectHeaders(req *http.Request, headers map[string]string) {
	for k, values := range c.headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("User-Agent", "Pippit-CLI/1.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

func (c *httpClient) prepareRequest(ctx context.Context, req *http.Request, headers map[string]string) error {
	trusted, err := c.isBaseURLOrigin(req.URL)
	if err != nil {
		return err
	}

	var ppeEnv string
	if trusted {
		if c.ppeEnv != nil {
			ppeEnv, err = config.NormalizePPEEnv(c.ppeEnv())
			if err != nil {
				return err
			}
		}
	}

	c.injectHeaders(req, headers)
	// Authentication and PPE routing are protected headers. Neither the
	// client's generic headers nor a caller-provided map may set or override
	// them; rebuild them below only for the configured API origin.
	req.Header.Del("Authorization")
	req.Header.Del(ppeUseHeader)
	req.Header.Del(ppeEnvHeader)
	req.Header.Del(ppeScheduleVDCHeader)
	if !trusted {
		// Absolute third-party URLs are used for result downloads. The protected
		// headers remain empty outside the API origin.
		return nil
	}
	if c.authorizer == nil {
		return fmt.Errorf("授权请求缺少认证器")
	}
	if err := c.authorizer.Inject(ctx, req); err != nil {
		return fmt.Errorf("写入认证请求头失败: %w", err)
	}
	if ppeEnv != "" {
		req.Header.Set(ppeUseHeader, "1")
		req.Header.Set(ppeEnvHeader, ppeEnv)
		req.Header.Set(ppeScheduleVDCHeader, defaultPPEVDC)
	}
	return nil
}

func (c *httpClient) isBaseURLOrigin(target *url.URL) (bool, error) {
	if c.baseURL == "" {
		return false, nil
	}
	base, err := url.Parse(c.baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return false, fmt.Errorf("解析 base URL %q 失败", c.baseURL)
	}
	return sameOrigin(base, target), nil
}

func (c *httpClient) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("停止重定向：已达到 10 次上限")
	}
	trusted, err := c.isBaseURLOrigin(req.URL)
	if err != nil {
		return err
	}
	initialTrusted, err := c.isBaseURLOrigin(via[0].URL)
	if err != nil {
		return err
	}
	if initialTrusted && !trusted {
		// API requests may contain unreleased canvas data or upload metadata.
		// Stripping credentials is insufficient because Go can preserve the
		// method and body for 307/308 redirects. API calls have no valid reason
		// to leave the configured origin, so reject the redirect entirely.
		return fmt.Errorf("拒绝 Pippit API 跨域重定向到 %s", req.URL.Redacted())
	}
	if trusted {
		// net/http may rebuild redirect headers from the original request. Once a
		// chain has crossed an untrusted origin, never restore protected headers
		// even if a later hop points back at the API origin.
		for _, previous := range via {
			previousTrusted, err := c.isBaseURLOrigin(previous.URL)
			if err != nil {
				return err
			}
			if !previousTrusted {
				trusted = false
				break
			}
		}
	}
	if !trusted {
		req.Header.Del("Authorization")
		req.Header.Del(ppeUseHeader)
		req.Header.Del(ppeEnvHeader)
		req.Header.Del(ppeScheduleVDCHeader)
	}
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func (c *httpClient) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s 请求失败: %w", req.Method, req.URL.String(), err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应体失败: %w", err)
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("%s %s 返回 HTTP %d: %s", req.Method, req.URL.String(), resp.StatusCode, msg)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := sonic.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析响应体失败: %w", err)
	}
	return nil
}

func (c *httpClient) resolveURL(path string, query map[string]string) (string, error) {
	parsed, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("解析 URL 失败: %w", err)
	}
	if parsed.IsAbs() {
		if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "", fmt.Errorf("仅支持 http 或 https URL: %q", path)
		}
		return appendQuery(path, query)
	}
	if c.baseURL == "" {
		return "", fmt.Errorf("相对路径 %q 需要配置 base URL", path)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return appendQuery(c.baseURL+path, query)
}

func appendQuery(raw string, query map[string]string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("解析 URL 失败: %w", err)
	}
	values := u.Query()
	for k, v := range query {
		values.Set(k, v)
	}
	u.RawQuery = values.Encode()
	return u.String(), nil
}
