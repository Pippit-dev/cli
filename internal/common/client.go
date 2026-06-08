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

	"github.com/bytedance/sonic"
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
}

type httpClient struct {
	baseURL    string
	httpClient *http.Client
	headers    http.Header
	authorizer RequestAuthorizer
}

func NewHTTPClient(baseURL string, timeout time.Duration, authorizer RequestAuthorizer) Client {
	return &httpClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		headers:    make(http.Header),
		authorizer: authorizer,
	}
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

	if c.authorizer == nil {
		return fmt.Errorf("授权请求缺少认证器")
	}
	if err := c.authorizer.Inject(ctx, req); err != nil {
		return fmt.Errorf("写入认证请求头失败: %w", err)
	}

	c.injectHeaders(req, headers)

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

	if c.authorizer == nil {
		_ = pr.Close()
		_ = pw.Close()
		return fmt.Errorf("授权请求缺少认证器")
	}
	if err := c.authorizer.Inject(ctx, req); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return fmt.Errorf("写入认证请求头失败: %w", err)
	}
	c.injectHeaders(req, nil)

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

	f, err := os.Open(file.Path)
	if err != nil {
		return fmt.Errorf("打开上传文件失败: %w", err)
	}
	defer f.Close()

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(file.FieldName), escapeQuotes(file.FileName)))
	header.Set("Content-Type", file.ContentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
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
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
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
