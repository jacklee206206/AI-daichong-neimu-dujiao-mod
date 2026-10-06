// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package domain

import (
	"testing"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestChannelCryptoTokenID(t *testing.T) {
	tests := []struct {
		name, provider, channelType string
		config                      jsonmap.JSON
		want                        string
	}{
		{"cashier BSC", "dujiaopay", "dujiaopay", jsonmap.JSON{"order_mode": "cashier", "allowed_methods": "bsc-usdt"}, "bsc-usdt"},
		{"normalized duplicate", " DujiaoPay ", "dujiaopay", jsonmap.JSON{"order_mode": " CASHIER ", "allowed_methods": " BSC-USDT, bsc-usdt, "}, "bsc-usdt"},
		{"multiple methods", "dujiaopay", "dujiaopay", jsonmap.JSON{"order_mode": "cashier", "allowed_methods": "bsc-usdt,tron-usdt"}, ""},
		{"unrestricted cashier ignores stale token", "dujiaopay", "dujiaopay", jsonmap.JSON{"order_mode": "cashier", "token_id": "bsc-usdt"}, ""},
		{"invalid method", "dujiaopay", "dujiaopay", jsonmap.JSON{"order_mode": "cashier", "allowed_methods": "bsc-usdt,invalid"}, ""},
		{"invalid config type", "dujiaopay", "dujiaopay", jsonmap.JSON{"order_mode": "cashier", "allowed_methods": []string{"bsc-usdt"}}, ""},
		{"fixed token takes precedence", "dujiaopay", "tron-usdt", jsonmap.JSON{"order_mode": "transaction", "token_id": "BSC-USDT"}, "bsc-usdt"},
		{"legacy transaction fallback", "dujiaopay", "tron-usdt", nil, "tron-usdt"},
		{"new hyphenated chain", "dujiaopay", "x-layer-usdt0", nil, "x-layer-usdt0"},
		{"invalid fixed token no fallback", "dujiaopay", "bsc-usdt", jsonmap.JSON{"token_id": "invalid"}, ""},
		{"not a token", "dujiaopay", "dujiaopay", nil, ""},
		{"invalid mode", "dujiaopay", "bsc-usdt", jsonmap.JSON{"order_mode": "other"}, ""},
		{"other provider", "epay", "bsc-usdt", jsonmap.JSON{"token_id": "bsc-usdt"}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := &PaymentChannel{ProviderType: test.provider, ChannelType: test.channelType, ConfigJSON: test.config}
			if got := channel.CryptoTokenID(); got != test.want {
				t.Fatalf("CryptoTokenID() = %q, want %q", got, test.want)
			}
		})
	}
	if got := (*PaymentChannel)(nil).CryptoTokenID(); got != "" {
		t.Fatalf("nil channel token = %q", got)
	}
}

func TestPaymentCryptoTokenSnapshotSurvivesChannelChange(t *testing.T) {
	channel := &PaymentChannel{ProviderType: constants.PaymentProviderDujiaoPay, ConfigJSON: jsonmap.JSON{"order_mode": "cashier", "allowed_methods": "tron-usdt"}}
	payment := &Payment{ProviderType: constants.PaymentProviderDujiaoPay}
	payment.SnapshotCryptoTokenID(channel)
	channel.ConfigJSON["allowed_methods"] = "bsc-usdt"
	if got := payment.CryptoTokenID(); got != "tron-usdt" {
		t.Fatalf("historical snapshot changed to %q", got)
	}
	legacy := &Payment{ProviderType: constants.PaymentProviderDujiaoPay, ProviderPayload: jsonmap.JSON{"token_id": "tron-usdt"}}
	if got := legacy.CryptoTokenID(); got != "" {
		t.Fatalf("legacy payment without snapshot must stay empty, got %q", got)
	}
	channel.ConfigJSON["allowed_methods"] = "bsc-usdt,tron-usdt"
	newPayment := &Payment{ProviderType: constants.PaymentProviderDujiaoPay, ProviderPayload: jsonmap.JSON{CryptoTokenSnapshotKey: "fake-usdt"}}
	newPayment.SnapshotCryptoTokenID(channel)
	if got := newPayment.CryptoTokenID(); got != "" {
		t.Fatalf("multi-method payment must not inherit gateway-supplied snapshot, got %q", got)
	}
}
