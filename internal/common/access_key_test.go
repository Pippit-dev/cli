package common

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestAccessKeyProviderAuthorizerReadsLatestValue(t *testing.T) {
	accessKey := " first-key "
	authorizer := NewAccessKeyProviderAuthorizer(func() string { return accessKey })

	first := newAccessKeyTestRequest(t, http.MethodPost)
	if err := authorizer.Inject(context.Background(), first); err != nil {
		t.Fatalf("Inject(first) error = %v", err)
	}
	if got := first.Header.Get("Authorization"); got != "Bearer first-key" {
		t.Fatalf("first Authorization = %q, want latest first key", got)
	}

	accessKey = "second-key"
	second := newAccessKeyTestRequest(t, http.MethodPost)
	if err := authorizer.Inject(context.Background(), second); err != nil {
		t.Fatalf("Inject(second) error = %v", err)
	}
	if got := second.Header.Get("Authorization"); got != "Bearer second-key" {
		t.Fatalf("second Authorization = %q, want updated key", got)
	}
}

func TestAccessKeyProviderAuthorizerRejectsMissingKeyWithoutLeakingPriorValue(t *testing.T) {
	accessKey := "prior-secret"
	authorizer := NewAccessKeyProviderAuthorizer(func() string { return accessKey })
	accessKey = "  "

	err := authorizer.Inject(context.Background(), newAccessKeyTestRequest(t, http.MethodPost))
	if err == nil || !strings.Contains(err.Error(), "XYQ_ACCESS_KEY 缺失") {
		t.Fatalf("Inject() error = %v, want missing Access Key guidance", err)
	}
	if strings.Contains(err.Error(), "prior-secret") {
		t.Fatalf("Inject() error leaks a previous Access Key: %v", err)
	}
}

func TestAccessKeyProviderAuthorizerAllowsUnauthenticatedRead(t *testing.T) {
	authorizer := NewAccessKeyProviderAuthorizer(nil)
	request := newAccessKeyTestRequest(t, http.MethodGet)

	if err := authorizer.Inject(context.Background(), request); err != nil {
		t.Fatalf("Inject() error = %v", err)
	}
	if got := request.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty", got)
	}
}

func TestAccessKeyAuthorizerKeepsConstantAPIBehavior(t *testing.T) {
	authorizer := NewAccessKeyAuthorizer(" constant-key ")
	request := newAccessKeyTestRequest(t, http.MethodPost)

	if err := authorizer.Inject(context.Background(), request); err != nil {
		t.Fatalf("Inject() error = %v", err)
	}
	if got := request.Header.Get("Authorization"); got != "Bearer constant-key" {
		t.Fatalf("Authorization = %q, want trimmed constant key", got)
	}
}

func newAccessKeyTestRequest(t *testing.T, method string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, "https://example.test/api", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	return request
}
