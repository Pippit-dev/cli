package common

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
)

type creditBalanceFakeClient struct {
	response string
	path     string
	body     any
}

func (c *creditBalanceFakeClient) SendRequest(_ context.Context, path string, body any, out any) error {
	c.path = path
	c.body = body
	return sonic.Unmarshal([]byte(c.response), out)
}

func (c *creditBalanceFakeClient) SendRequestWithHeaders(ctx context.Context, path string, body any, _ map[string]string, out any) error {
	return c.SendRequest(ctx, path, body, out)
}

func (c *creditBalanceFakeClient) SendMultipartRequest(context.Context, string, map[string]string, MultipartFile, any) error {
	return fmt.Errorf("unexpected multipart request")
}

func TestGetCreditBalanceReturnsAmount(t *testing.T) {
	client := &creditBalanceFakeClient{
		response: `{"ret":"0","log_id":"log_balance","data":{"total_remain_amount":"4321"}}`,
	}

	result, err := GetCreditBalance(context.Background(), &Runner{Client: client})
	if err != nil {
		t.Fatalf("GetCreditBalance() error = %v", err)
	}
	if result.TotalRemainAmount != 4321 {
		t.Fatalf("TotalRemainAmount = %d, want 4321", result.TotalRemainAmount)
	}
	if result.LogID != "log_balance" {
		t.Fatalf("LogID = %q, want log_balance", result.LogID)
	}
	if client.path != "/api/biz/v1/skill/get_credit_balance" {
		t.Fatalf("path = %q, want get_credit_balance path", client.path)
	}
	if _, ok := client.body.(struct{}); !ok {
		t.Fatalf("body = %#v, want an empty POST body", client.body)
	}
}

func TestGetCreditBalanceRequiresOptionalAmount(t *testing.T) {
	client := &creditBalanceFakeClient{
		response: `{"ret":"0","log_id":"log_missing","data":{}}`,
	}

	_, err := GetCreditBalance(context.Background(), &Runner{Client: client})
	if err == nil || !strings.Contains(err.Error(), "data.total_remain_amount") || !strings.Contains(err.Error(), "log_id=log_missing") {
		t.Fatalf("GetCreditBalance() error = %v, want missing amount and log_id", err)
	}
}

func TestGetCreditBalanceBusinessErrorIncludesLogID(t *testing.T) {
	client := &creditBalanceFakeClient{
		response: `{"ret":"16008","errmsg":"查询失败","log_id":"log_123"}`,
	}

	_, err := GetCreditBalance(context.Background(), &Runner{Client: client})
	if err == nil || !strings.Contains(err.Error(), "ret=16008 errmsg=查询失败") || !strings.Contains(err.Error(), "log_id=log_123") {
		t.Fatalf("GetCreditBalance() error = %v, want business error and log_id", err)
	}
}
