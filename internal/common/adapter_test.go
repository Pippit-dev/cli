package common

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Pippit-dev/pippit-cli/internal/adapter"
)

type decisionPolicy struct {
	adapter.Default
	decision           adapter.AuthDecision
	err                error
	prepared, stripped int
}

func (p *decisionPolicy) PrepareAPIRequest(context.Context, *http.Request) (adapter.AuthDecision, error) {
	p.prepared++
	return p.decision, p.err
}
func (p *decisionPolicy) StripSensitiveHeaders(req *http.Request) {
	p.stripped++
	req.Header.Del("X-Adapter-Auth")
}

// The common client handles authentication decisions and origin checks. Adapter errors must not trigger a fallback to local authentication.
func TestAdapterAuthenticationBoundary(t *testing.T) {
	failed := errors.New("adapter rejected")
	for _, tc := range []struct {
		name, target                     string
		decision                         adapter.AuthDecision
		policyErr                        error
		wantAuth, wantPrepare, wantStrip bool
		wantError                        bool
	}{
		{name: "default", target: "https://api.example.test/path", wantAuth: true, wantPrepare: true},
		{name: "provided", target: "https://api.example.test/path", decision: adapter.AuthProvided, wantPrepare: true},
		{name: "error", target: "https://api.example.test/path", policyErr: failed, wantPrepare: true, wantError: true},
		{name: "invalid", target: "https://api.example.test/path", decision: adapter.AuthDecision(99), wantPrepare: true, wantError: true},
		{name: "external", target: "https://files.example.test/file", wantStrip: true},
		{name: "different port", target: "https://api.example.test:8443/file", wantStrip: true},
		{name: "different scheme", target: "http://api.example.test/file", wantStrip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := &decisionPolicy{decision: tc.decision, err: tc.policyErr}
			authCalls := 0
			authorizer := NewAccessKeyContextProviderAuthorizer(func(context.Context) (string, error) { authCalls++; return "local-key", nil })
			client := NewHTTPClientWithAdapter("https://api.example.test", time.Second, authorizer, policy).(*httpClient)
			req, err := http.NewRequest(http.MethodGet, tc.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = client.prepareRequest(context.Background(), req, map[string]string{"Authorization": "Bearer caller", "X-Adapter-Auth": "caller"})
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			if tc.policyErr != nil && !errors.Is(err, failed) {
				t.Fatalf("adapter error lost: %v", err)
			}
			if (authCalls == 1) != tc.wantAuth || (policy.prepared == 1) != tc.wantPrepare || (policy.stripped == 1) != tc.wantStrip {
				t.Fatalf("auth=%d prepare=%d strip=%d", authCalls, policy.prepared, policy.stripped)
			}
			if !tc.wantAuth && req.Header.Get("Authorization") != "" {
				t.Fatal("caller Authorization leaked")
			}
			if tc.wantStrip && req.Header.Get("X-Adapter-Auth") != "" {
				t.Fatal("external request retained adapter credentials")
			}
		})
	}
}
