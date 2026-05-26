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
			return fmt.Errorf("encode body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.authorizer == nil {
		return fmt.Errorf("authorized request requires authorizer")
	}
	if err := c.authorizer.Inject(ctx, req); err != nil {
		return fmt.Errorf("inject auth headers: %w", err)
	}

	c.injectHeaders(req)

	// If out is **http.Response, return the raw response for streaming (e.g. file download).
	if out != nil {
		if rv := reflect.ValueOf(out); rv.Kind() == reflect.Ptr && rv.Elem().Kind() == reflect.Ptr {
			if rv.Elem().Type().Elem() == reflect.TypeOf(http.Response{}) {
				resp, err := c.httpClient.Do(req)
				if err != nil {
					return fmt.Errorf("%s %s failed: %w", method, reqURL, err)
				}
				if resp.StatusCode >= 400 {
					defer resp.Body.Close()
					return fmt.Errorf("%s %s returned HTTP %d", method, reqURL, resp.StatusCode)
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
		return fmt.Errorf("multipart file field name is required")
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
		return fmt.Errorf("build POST request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	if c.authorizer == nil {
		_ = pr.Close()
		_ = pw.Close()
		return fmt.Errorf("authorized request requires authorizer")
	}
	if err := c.authorizer.Inject(ctx, req); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return fmt.Errorf("inject auth headers: %w", err)
	}
	c.injectHeaders(req)

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
		return fmt.Errorf("open upload file: %w", err)
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
		return fmt.Errorf("write upload file: %w", err)
	}
	return nil
}

var quotesReplacer = strings.NewReplacer("\\", "\\\\", `"`, `\"`)

func escapeQuotes(s string) string {
	return quotesReplacer.Replace(s)
}

func (c *httpClient) injectHeaders(req *http.Request) {
	for k, values := range c.headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("User-Agent", "Pippit-CLI/1.0")
	req.Header.Set("x-use-ppe", "1")
	req.Header.Set("x-tt-env", "ppe_harness_novel_v2")
}

func (c *httpClient) do(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s failed: %w", req.Method, req.URL.String(), err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("%s %s returned HTTP %d: %s", req.Method, req.URL.String(), resp.StatusCode, msg)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := sonic.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}

func (c *httpClient) resolveURL(path string, query map[string]string) (string, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return appendQuery(path, query)
	}
	if c.baseURL == "" {
		return "", fmt.Errorf("base URL is required for relative path %q", path)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return appendQuery(c.baseURL+path, query)
}

func appendQuery(raw string, query map[string]string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	values := u.Query()
	for k, v := range query {
		values.Set(k, v)
	}
	u.RawQuery = values.Encode()
	return u.String(), nil
}
