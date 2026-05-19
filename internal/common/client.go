package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/auth"
	"github.com/bytedance/sonic"
)

var (
	defaultOnce   sync.Once
	defaultClient *Client
)

// Client is the shared HTTP client used by command modules.
type Client struct {
	mu sync.RWMutex

	BaseURL    string
	HTTPClient *http.Client
	Headers    http.Header
}

// NewClient returns the process-wide shared HTTP client. Repeated calls return the
// same instance and update its base URL for the current command invocation.
func NewClient(baseURL string, timeout ...time.Duration) *Client {
	defaultOnce.Do(func() {
		defaultClient = &Client{
			Headers: make(http.Header),
		}
	})
	defaultClient.SetBaseURL(baseURL)
	if len(timeout) > 0 {
		defaultClient.SetTimeout(timeout[0])
	}
	return defaultClient
}

// SetBaseURL updates the shared client's base URL.
func (c *Client) SetBaseURL(baseURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.BaseURL = strings.TrimRight(baseURL, "/")
}

// SetHeader sets a default header applied to every request.
func (c *Client) SetHeader(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Headers.Set(key, value)
}

func (c *Client) SetTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{}
	}
	c.HTTPClient.Timeout = timeout
}

func (c *Client) snapshot() (string, *http.Client, http.Header) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	headers := make(http.Header, len(c.Headers))
	for k, values := range c.Headers {
		headers[k] = append([]string(nil), values...)
	}
	return c.BaseURL, c.HTTPClient, headers
}

// Get issues a GET request and JSON-decodes the response into out when out is non-nil.
func (c *Client) Get(ctx context.Context, path string, query map[string]string, out any) error {
	reqURL, err := c.resolveURL(path, query)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("build GET request: %w", err)
	}
	return c.do(req, out)
}

// Post issues a JSON POST request and JSON-decodes the response into out when out is non-nil.
func (c *Client) Post(ctx context.Context, path string, body any, out any) error {
	req, err := c.newJSONRequest(ctx, path, body)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) PostAuthenticated(ctx context.Context, path string, body any, out any, authorizer auth.Authorizer, ensureTTL time.Duration) error {
	if authorizer == nil {
		return fmt.Errorf("authenticated request requires authorizer")
	}
	req, err := c.newJSONRequest(ctx, path, body)
	if err != nil {
		return err
	}
	if err := authorizer.Refresh(ctx, ensureTTL); err != nil {
		return fmt.Errorf("refresh auth state: %w", err)
	}
	if err := authorizer.Inject(ctx, req); err != nil {
		return fmt.Errorf("inject auth headers: %w", err)
	}
	return c.do(req, out)
}

func (c *Client) newJSONRequest(ctx context.Context, path string, body any) (*http.Request, error) {
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

func (c *Client) do(req *http.Request, out any) error {
	_, httpClient, headers := c.snapshot()
	for k, values := range headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Accept", "application/json")

	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
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

func (c *Client) resolveURL(path string, query map[string]string) (string, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return appendQuery(path, query)
	}
	baseURL, _, _ := c.snapshot()
	if baseURL == "" {
		return "", fmt.Errorf("base URL is required for relative path %q", path)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return appendQuery(baseURL+path, query)
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
