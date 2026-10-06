// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
)

// Exercise the actual HTTP adapter against the flat JSON contracts emitted by
// upstreamhttp.CreateOrder/GetOrder. All values and the card are test fixtures.
func TestDujiaoNextOrderHTTPContract(t *testing.T) {
	const secret = "fixture-order-secret"
	const callbackURL = "https://example.test/api/v1/upstream/callback"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		ts, err := strconv.ParseInt(r.Header.Get(HeaderTimestamp), 10, 64)
		if err != nil || r.Header.Get(HeaderApiKey) != "fixture-order-key" || !Verify(secret, r.Method, r.URL.Path, r.Header.Get(HeaderSignature), ts, body) {
			t.Error("adapter request signature/header contract mismatch")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/upstream/orders":
			var request map[string]any
			if err := json.Unmarshal(body, &request); err != nil {
				t.Error(err)
				return
			}
			if request["downstream_order_no"] != "LOCAL-FIXTURE-1" || request["trace_id"] != "TRACE-FIXTURE-1" || request["callback_url"] != callbackURL || request["sku_id"] != float64(201) || request["quantity"] != float64(1) {
				t.Error("adapter create order request field contract mismatch")
			}
			if _, exists := request["local_order_no"]; exists {
				t.Error("wire protocol must use downstream_order_no rather than local_order_no")
			}
			_, _ = w.Write([]byte(`{"ok":true,"order_id":999,"order_no":"UP-FIXTURE-999","status":"paid","amount":"98.00","currency":"CNY"}`))
		case "GET /api/v1/upstream/orders/999":
			_, _ = w.Write([]byte(`{"ok":true,"order_id":999,"order_no":"UP-FIXTURE-999","status":"delivered","amount":"98.00","refunded_amount":"0.00","currency":"CNY","refund_records":[],"fulfillment":{"type":"auto","status":"delivered","payload":"FIXTURE-NOT-A-REAL-CARD","delivery_data":{"redeem_url":"https://example.test/redeem"},"delivered_at":"2026-10-03T10:00:00Z"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	adapter := NewDujiaoNextAdapter(&siteconnectiondomain.Connection{BaseURL: server.URL, ApiKey: "fixture-order-key", ApiSecret: secret}, t.TempDir())
	created, err := adapter.CreateOrder(context.Background(), CreateUpstreamOrderReq{SKUID: 201, Quantity: 1, DownstreamOrderNo: "LOCAL-FIXTURE-1", TraceID: "TRACE-FIXTURE-1", CallbackURL: callbackURL})
	if err != nil {
		t.Fatal(err)
	}
	if !created.OK || created.OrderID != 999 || created.Status != "paid" || created.Amount != "98.00" || created.Currency != "CNY" {
		t.Fatalf("flat create response not decoded correctly: %#v", created)
	}
	detail, err := adapter.GetOrder(context.Background(), created.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != "delivered" || detail.Fulfillment == nil || detail.Fulfillment.Payload != "FIXTURE-NOT-A-REAL-CARD" || detail.Fulfillment.Status != "delivered" || detail.Fulfillment.DeliveredAt == nil || detail.Fulfillment.DeliveryData["redeem_url"] != "https://example.test/redeem" {
		t.Fatal("flat detail response or nested fulfillment payload not decoded correctly")
	}
}
