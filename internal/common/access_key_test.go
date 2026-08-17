package common

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	internal_auth "github.com/Pippit-dev/pippit-cli/internal/auth"
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
	if err == nil || !strings.Contains(err.Error(), "pippit-tool-cli login") {
		t.Fatalf("Inject() error = %v, want missing Access Key guidance", err)
	}
	if strings.Contains(err.Error(), "prior-secret") {
		t.Fatalf("Inject() error leaks a previous Access Key: %v", err)
	}
}

func TestAccessKeyContextProviderAuthorizerLoadsSavedCredentialLazily(t *testing.T) {
	called := 0
	authorizer := NewAccessKeyContextProviderAuthorizer(func(ctx context.Context) (string, error) {
		called++
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return " saved-browser-key ", nil
	})
	request := newAccessKeyTestRequest(t, http.MethodPost)
	if called != 0 {
		t.Fatal("credential provider was called before request authorization")
	}
	if err := authorizer.Inject(context.Background(), request); err != nil {
		t.Fatalf("Inject() error = %v", err)
	}
	if called != 1 || request.Header.Get("Authorization") != "Bearer saved-browser-key" {
		t.Fatalf("called=%d Authorization=%q", called, request.Header.Get("Authorization"))
	}
}

func TestAccessKeyContextProviderAuthorizerPreservesCredentialStateErrors(t *testing.T) {
	for _, cause := range []error{internal_auth.ErrCredentialNotFound, internal_auth.ErrCredentialExpired} {
		authorizer := NewAccessKeyContextProviderAuthorizer(func(context.Context) (string, error) {
			return "", cause
		})
		err := authorizer.Inject(context.Background(), newAccessKeyTestRequest(t, http.MethodPost))
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "pippit-tool-cli login") {
			t.Fatalf("Inject() error = %v, want wrapped %v", err, cause)
		}
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
