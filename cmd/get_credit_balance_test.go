package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetCreditBalanceCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/biz/v1/skill/get_credit_balance" {
			t.Fatalf("path = %s, want get_credit_balance path", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want test bearer token", r.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != "{}" {
			t.Fatalf("body = %s, want empty JSON object", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":"0","data":{"total_remain_amount":"0"}}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{"get-credit-balance"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if got, want := stdout.String(), "{\"total_remain_amount\":\"0\"}\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestGetCreditBalanceCommandRejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"get-credit-balance", "unexpected"})

	if err := root.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want argument rejection")
	}
}

func TestGetCreditBalanceCommandSupportsLogID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":"0","log_id":"log_123","data":{"total_remain_amount":"48592"}}`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	root := newTestRootCommand(t, &stdout, &stderr, server.URL)
	root.SetArgs([]string{"get-credit-balance", "--with-log-id"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v, stderr = %s", err, stderr.String())
	}
	if got, want := stdout.String(), "{\"total_remain_amount\":\"48592\",\"log_id\":\"log_123\"}\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}
