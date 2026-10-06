// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package channels_test

import (
	"encoding/json"
	"strings"
	"testing"

	paymentapp "github.com/dujiao-next/internal/modules/payment/application"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestAvailableChannelsExposeOnlyUniqueCryptoToken(t *testing.T) {
	svc, _, db := setupAvailableChannelService(t)
	for _, fixture := range []struct{ name, methods string }{
		{"bsc", "bsc-usdt"},
		{"multiple", "bsc-usdt,tron-usdt"},
		{"unrestricted", ""},
	} {
		createAvailableChannelFixture(t, db, paymentdomain.PaymentChannel{
			Name: fixture.name, ProviderType: "dujiaopay", ChannelType: "dujiaopay", IsActive: true,
			ConfigJSON: jsonmap.JSON{"order_mode": "cashier", "allowed_methods": fixture.methods, "api_secret": "must-not-leak"},
		})
	}
	channels, err := svc.GetAvailableChannels(paymentapp.AvailablePaymentChannelFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 3 {
		t.Fatalf("got %d channels, want 3", len(channels))
	}
	for _, channel := range channels {
		value, exists := channel["crypto_token_id"]
		if channel["name"] == "bsc" {
			if value != "bsc-usdt" {
				t.Fatalf("BSC token = %v", value)
			}
		} else if exists {
			t.Fatalf("ambiguous channel exposed token: %v", value)
		}
		data, err := json.Marshal(channel)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "must-not-leak") || strings.Contains(string(data), "config_json") || strings.Contains(string(data), "allowed_methods") {
			t.Fatal("public channel exposed private configuration")
		}
	}
}
