package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bytedance/sonic"
)

type Client interface {
	SendRequest(ctx context.Context, path string, body any, out any) error
}

type RequestAuthorizer interface {
	Inject(ctx context.Context, req *http.Request) error
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
	if c.authorizer == nil {
		return fmt.Errorf("authorized request requires authorizer")
	}
	req, err := c.newJSONRequest(ctx, path, body)
	if err != nil {
		return err
	}
	if err := c.authorizer.Inject(ctx, req); err != nil {
		return fmt.Errorf("inject auth headers: %w", err)
	}
	return c.do(req, out)
}

func (c *httpClient) newJSONRequest(ctx context.Context, path string, body any) (*http.Request, error) {
	reqURL, err := c.resolveURL(path, nil)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		payload, err := sonic.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode POST body: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, reader)
	if err != nil {
		return nil, fmt.Errorf("build POST request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *httpClient) do(req *http.Request, out any) error {
	for k, values := range c.headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Accept", "application/json")

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
