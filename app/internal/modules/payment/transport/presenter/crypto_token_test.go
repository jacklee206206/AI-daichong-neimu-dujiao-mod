// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package presenter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/constants"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestPaymentResponsesUseCryptoCreationSnapshot(t *testing.T) {
	channel := &paymentdomain.PaymentChannel{ProviderType: "dujiaopay", ConfigJSON: jsonmap.JSON{"order_mode": "cashier", "allowed_methods": "bsc-usdt", "api_secret": "must-not-leak"}}
	for _, test := range []struct {
		name, mode, snapshot, actualToken, want string
	}{
		{"new cashier", "redirect", "bsc-usdt", "", "bsc-usdt"},
		{"historical TRON snapshot", "redirect", "tron-usdt", "", "tron-usdt"},
		{"legacy without snapshot", "redirect", "", "", ""},
		{"QR keeps actual token separate", "qr", "bsc-usdt", "tron-usdt", "bsc-usdt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payment := &paymentdomain.Payment{ProviderType: constants.PaymentProviderDujiaoPay, InteractionMode: test.mode, ProviderPayload: jsonmap.JSON{"token_id": test.actualToken, "api_secret": "must-not-leak"}}
			if test.snapshot != "" {
				payment.ProviderPayload[paymentdomain.CryptoTokenSnapshotKey] = test.snapshot
			}
			created := NewCreatePaymentResp(&CreatePaymentResultView{Payment: payment, Channel: channel})
			latest := NewLatestPaymentResp(payment, "TEST-ORDER")
			if created.CryptoTokenID != test.want || latest.CryptoTokenID != test.want {
				t.Fatalf("snapshot mismatch: created=%q latest=%q want=%q", created.CryptoTokenID, latest.CryptoTokenID, test.want)
			}
			if created.TokenID != test.actualToken || latest.TokenID != test.actualToken {
				t.Fatalf("actual token was overwritten: created=%q latest=%q", created.TokenID, latest.TokenID)
			}
			for _, response := range []any{created, latest} {
				data, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				text := string(data)
				if strings.Contains(text, "must-not-leak") || strings.Contains(text, "config_json") || strings.Contains(text, paymentdomain.CryptoTokenSnapshotKey) {
					t.Fatal("response leaked private configuration or payload")
				}
				if test.want == "" && strings.Contains(text, "crypto_token_id") {
					t.Fatal("empty snapshot must be omitted")
				}
			}
		})
	}
}
