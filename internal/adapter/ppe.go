package adapter

import (
	"context"
	"net/http"
)

// WithPPE applies runtime routing only to requests accepted by the common
// client's API-origin check, while preserving the supplied adapter's policies.
func WithPPE(policy HTTPAdapter, env string) HTTPAdapter {
	policy = HTTPOrDefault(policy)
	if env == "" {
		return policy
	}
	return ppeAdapter{HTTPAdapter: policy, env: env}
}

type ppeAdapter struct {
	HTTPAdapter
	env string
}

func (a ppeAdapter) PrepareAPIRequest(ctx context.Context, req *http.Request) (AuthDecision, error) {
	decision, err := a.HTTPAdapter.PrepareAPIRequest(ctx, req)
	if err == nil {
		req.Header.Set("x-tt-env", a.env)
		req.Header.Set("x-use-ppe", "1")
	}
	return decision, err
}

func (a ppeAdapter) StripSensitiveHeaders(req *http.Request) {
	a.HTTPAdapter.StripSensitiveHeaders(req)
	req.Header.Del("x-tt-env")
	req.Header.Del("x-use-ppe")
}
