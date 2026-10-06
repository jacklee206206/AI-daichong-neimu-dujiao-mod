// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"testing"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestMergeProviderPayloadProtectsCryptoSnapshot(t *testing.T) {
	incoming := jsonmap.JSON{paymentdomain.CryptoTokenSnapshotKey: "bsc-usdt", "token_id": "tron-usdt"}
	existing := jsonmap.JSON{paymentdomain.CryptoTokenSnapshotKey: "tron-usdt"}
	merged := mergeProviderPayload(existing, incoming)
	if merged[paymentdomain.CryptoTokenSnapshotKey] != "tron-usdt" || merged["token_id"] != "tron-usdt" {
		t.Fatal("callback must preserve local snapshot while accepting actual token data")
	}
	legacy := mergeProviderPayload(nil, incoming)
	if _, exists := legacy[paymentdomain.CryptoTokenSnapshotKey]; exists {
		t.Fatal("callback must not create a local snapshot for legacy payments")
	}
}
