package common

import (
	"encoding/json"
	"testing"
)

func TestRunCreditSettlement(t *testing.T) {
	for _, tt := range []struct {
		name   string
		state  int
		ledger string
		want   string
	}{
		{"pending", 2, `{"status":3,"actual_deduct_credits":"0","pre_deduct_credits":"7"}`, `{"deducted":null,"preDeducted":7,"settlementStatus":3}`},
		{"historical without row", 3, `{"status":1,"actual_deduct_credits":"0"}`, `{"deducted":null,"settlementStatus":1}`},
		{"free", 3, `{"status":1,"actual_deduct_credits":"0","updated_at_ms":"1"}`, `{"deducted":0,"settlementStatus":1}`},
		{"refunded failure", 4, `{"status":6,"actual_deduct_credits":"0","returned_credits":"4"}`, `{"deducted":0,"returned":4,"settlementStatus":6}`},
		{"invalid amounts", 3, `{"status":4,"actual_deduct_credits":"-1","returned_credits":1.2}`, `{"deducted":null,"settlementStatus":4}`},
		{"missing", 3, `{}`, `{"deducted":null}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var snapshot runCreditSnapshot
			if err := json.Unmarshal([]byte(tt.ledger), &snapshot); err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(snapshot.credits(tt.state))
			if err != nil || string(got) != tt.want {
				t.Fatalf("credits = %s, err = %v, want %s", got, err, tt.want)
			}
		})
	}
}
