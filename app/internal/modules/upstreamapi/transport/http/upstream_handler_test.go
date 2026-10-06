// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package upstreamhttp

import (
	"github.com/dujiao-next/internal/constants"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"

	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	upstreamadapter "github.com/dujiao-next/internal/upstream"

	"github.com/gin-gonic/gin"
)

func TestMapCallbackStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{name: "delivered keep delivered", input: "delivered", expect: "delivered"},
		{name: "completed map delivered", input: "completed", expect: "delivered"},
		{name: "fulfilled map delivered", input: "fulfilled", expect: "delivered"},
		{name: "canceled keep canceled", input: "canceled", expect: "canceled"},
		{name: "cancelled map canceled", input: "cancelled", expect: "canceled"},
		{name: "refunded keep refunded", input: "refunded", expect: "refunded"},
		{name: "partially refunded keep value", input: "partially_refunded", expect: "partially_refunded"},
		{name: "trim and lower", input: "  ReFuNdEd  ", expect: "refunded"},
		{name: "fallback normalized raw", input: "PROCESSING", expect: "processing"},
	}

	for _, tc := range tests {
		got := mapCallbackStatus(tc.input)
		if got != tc.expect {
			t.Fatalf("%s: want %q got %q", tc.name, tc.expect, got)
		}
	}
}

type stubConnections struct {
	conn *siteconnectiondomain.Connection
}

func (s stubConnections) GetByApiKey(string) (*siteconnectiondomain.Connection, error) {
	return s.conn, nil
}

type stubSecrets struct{}

func (stubSecrets) DecryptSecret(encrypted string) (string, error) { return encrypted, nil }

type stubProcurements struct {
	order   *procurementdomain.Order
	handled bool
}

func (s *stubProcurements) GetByLocalOrderNo(string) (*procurementdomain.Order, error) {
	return s.order, nil
}

func (s *stubProcurements) HandleUpstreamCallback(uint, string, *procurementcontract.Fulfillment) error {
	s.handled = true
	return nil
}

// TestHandleCallbackOwnership 保证回调只能由采购单所属连接、且上游订单号一致时才被受理。
func TestHandleCallbackOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name         string
		connectionID uint
		upstreamID   uint
		payloadID    uint
		wantHandled  bool
	}{
		{name: "同连接同上游单号放行", connectionID: 1, upstreamID: 88, payloadID: 88, wantHandled: true},
		{name: "上游单号未落库时只校验连接", connectionID: 1, upstreamID: 0, payloadID: 88, wantHandled: true},
		{name: "其它连接冒用本地订单号", connectionID: 2, upstreamID: 88, payloadID: 88, wantHandled: false},
		{name: "上游订单号不匹配", connectionID: 1, upstreamID: 88, payloadID: 99, wantHandled: false},
	}

	for _, tc := range cases {
		procurements := &stubProcurements{order: &procurementdomain.Order{
			ID:              7,
			ConnectionID:    tc.connectionID,
			LocalOrderNo:    "LOCAL-1",
			UpstreamOrderID: tc.upstreamID,
		}}
		handler := &Handler{Dependencies: Dependencies{
			Connections: stubConnections{conn: &siteconnectiondomain.Connection{
				ID: 1, ApiKey: "key-1", ApiSecret: "secret-1", Status: "active",
			}},
			ConnectionSecrets: stubSecrets{},
			Procurements:      procurements,
		}}

		timestamp := time.Now().Unix()
		body, _ := json.Marshal(callbackPayload{
			Event:             "order.fulfilled",
			OrderID:           tc.payloadID,
			DownstreamOrderNo: "LOCAL-1",
			Status:            "delivered",
			Timestamp:         timestamp,
		})

		request := httptest.NewRequest(http.MethodPost, "/api/v1/upstream/callback", bytes.NewReader(body))
		request.Header.Set(upstreamadapter.HeaderApiKey, "key-1")
		request.Header.Set(upstreamadapter.HeaderTimestamp, fmt.Sprintf("%d", timestamp))
		request.Header.Set(upstreamadapter.HeaderSignature,
			upstreamadapter.Sign("secret-1", http.MethodPost, "/api/v1/upstream/callback", timestamp, body))

		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = request
		handler.HandleCallback(c)

		if procurements.handled != tc.wantHandled {
			t.Fatalf("%s: handled=%v want %v (body %s)", tc.name, procurements.handled, tc.wantHandled, recorder.Body.String())
		}
	}
}

type refundWireOrders struct {
	order   *orderdomain.Order
	records []jsonmap.JSON
}

func (s refundWireOrders) CreateOrder(CreateOrderInput) (*orderdomain.Order, error) {
	panic("unexpected create")
}
func (s refundWireOrders) GetOrderByUser(orderID, userID uint) (*orderdomain.Order, error) {
	if orderID != s.order.ID || userID != s.order.UserID {
		return nil, ErrOrderNotFound
	}
	return s.order, nil
}
func (s refundWireOrders) CancelOrder(uint, uint) (*orderdomain.Order, error) {
	panic("unexpected cancel")
}
func (s refundWireOrders) BuildLocalRefundRecordsForOrder(*orderdomain.Order) ([]jsonmap.JSON, error) {
	return s.records, nil
}

func TestGetOrderSeparatesSupplierRefundFromCustomerRefund(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, localStatus := range []string{constants.OrderStatusDelivered, constants.OrderStatusCompleted, constants.OrderStatusPartiallyRefunded, constants.OrderStatusRefunded} {
		for _, procurementStatus := range []string{constants.ProcurementStatusPartiallyRefunded, constants.ProcurementStatusRefunded} {
			t.Run(localStatus+"/supplier_"+procurementStatus, func(t *testing.T) {
				refunded := decimal.Zero
				records := make([]jsonmap.JSON, 0)
				if localStatus == constants.OrderStatusPartiallyRefunded {
					refunded = decimal.NewFromInt(25)
				} else if localStatus == constants.OrderStatusRefunded {
					refunded = decimal.NewFromInt(125)
				}
				if refunded.IsPositive() {
					records = append(records, jsonmap.JSON{"amount": refunded.StringFixed(2)})
				}
				order := &orderdomain.Order{
					ID: 7, UserID: 42, OrderNo: "LOCAL-7", Status: localStatus, Currency: "CNY",
					TotalAmount: money.FromDecimal(decimal.NewFromInt(125)), RefundedAmount: money.FromDecimal(refunded),
					Fulfillment: &fulfillmentdomain.Fulfillment{Type: constants.FulfillmentTypeUpstream, Status: constants.FulfillmentStatusDelivered, Payload: "FIXTURE-NOT-A-REAL-CARD"},
				}
				handler := &Handler{Dependencies: Dependencies{
					Orders: refundWireOrders{order: order, records: records},
					Procurements: &stubProcurements{order: &procurementdomain.Order{
						ID: 9, LocalOrderID: order.ID, LocalOrderNo: order.OrderNo, Status: procurementStatus,
					}},
				}}
				router := gin.New()
				router.Use(func(c *gin.Context) { c.Set(upstreamUserIDKey, order.UserID); c.Next() })
				router.GET("/api/v1/upstream/orders/:id", handler.GetOrder)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/upstream/orders/7", nil))
				if recorder.Code != http.StatusOK {
					t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
				}
				var response struct {
					Status         string         `json:"status"`
					RefundedAmount string         `json:"refunded_amount"`
					RefundRecords  []jsonmap.JSON `json:"refund_records"`
					Fulfillment    map[string]any `json:"fulfillment"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Status != localStatus || response.RefundedAmount != refunded.StringFixed(2) || len(response.RefundRecords) != len(records) {
					t.Fatalf("supplier refund replaced local facts: %s", recorder.Body.String())
				}
				if response.Fulfillment["payload"] != "FIXTURE-NOT-A-REAL-CARD" {
					t.Fatal("delivery evidence was lost")
				}
			})
		}
	}
}
