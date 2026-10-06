// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package procurement_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	procurementapp "github.com/dujiao-next/internal/modules/procurement/application"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementgormstore "github.com/dujiao-next/internal/modules/procurement/infrastructure/gormstore"
	procurementupstream "github.com/dujiao-next/internal/modules/procurement/infrastructure/upstreamgateway"
	siteconnectionapp "github.com/dujiao-next/internal/modules/siteconnection/application"
)

// ── PollUpstreamStatus test ──

func TestPollUpstreamStatus_Delivered(t *testing.T) {
	db := setupProcurementTestDB(t)

	order := createProcTestOrder(t, db, "PROC-POLL-001", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		now := time.Now()
		json.NewEncoder(w).Encode(map[string]any{
			"order_id": 999,
			"order_no": "UP-999",
			"status":   "delivered",
			"amount":   "50.00",
			"currency": "CNY",
			"fulfillment": map[string]any{
				"type":         "auto",
				"status":       "delivered",
				"payload":      "KEY-001\nKEY-002",
				"delivered_at": now.Format(time.RFC3339),
			},
		})
	}))
	defer server.Close()

	connSvc := newTestSiteConnectionService(db, "test-key", t.TempDir())
	conn, _ := connSvc.Create(siteconnectionapp.CreateInput{
		Name: "poll-upstream", BaseURL: server.URL,
		ApiKey: "key", ApiSecret: "secret", Protocol: constants.ConnectionProtocolDujiaoNext,
	})

	proc := createTestProcurementOrder(t, db, conn.ID, order.ID, order.OrderNo, "accepted")
	db.Model(proc).Updates(map[string]interface{}{
		"upstream_order_id": uint(999),
		"upstream_order_no": "UP-999",
	})

	svc := newTestProcurementService(db, connSvc)

	if err := svc.PollUpstreamStatus(proc.ID); err != nil {
		t.Fatalf("PollUpstreamStatus: %v", err)
	}

	// 验证采购单状态 = fulfilled
	var updatedProc ProcurementOrder
	db.First(&updatedProc, proc.ID)
	if updatedProc.Status != "fulfilled" {
		t.Errorf("expected procurement status 'fulfilled', got %q", updatedProc.Status)
	}

	// 验证本地订单状态 = delivered
	var updatedOrder orderdomain.Order
	db.First(&updatedOrder, order.ID)
	if updatedOrder.Status != constants.OrderStatusDelivered {
		t.Errorf("expected order status %q, got %q", constants.OrderStatusDelivered, updatedOrder.Status)
	}
}

type countingPollQueue struct{ polls int }

func (q *countingPollQueue) EnqueueSubmit(uint, ...time.Duration) error { return nil }
func (q *countingPollQueue) EnqueuePoll(uint, time.Duration) error      { q.polls++; return nil }

func TestPollUpstreamStatus_DoesNotOverwriteConcurrentCallback(t *testing.T) {
	for _, tc := range []struct {
		name, callback, want string
		queryFails           bool
	}{
		{name: "delivered", callback: "delivered", want: constants.ProcurementStatusFulfilled},
		{name: "refunded", callback: "refunded", want: constants.ProcurementStatusRefunded},
		{name: "canceled", callback: "canceled", want: constants.ProcurementStatusCanceled},
		{name: "failed_query_after_refund", callback: "refunded", want: constants.ProcurementStatusRefunded, queryFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupProcurementTestDB(t)
			order := createProcTestOrder(t, db, "POLL-CALLBACK-RACE", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
			queryStarted, releaseResponse := make(chan struct{}), make(chan struct{})
			var release sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(queryStarted)
				<-releaseResponse
				if tc.queryFails {
					http.Error(w, "temporary query failure", http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"order_id": 123, "order_no": "UP-123", "status": "paid", "amount": "1.00", "currency": "CNY"})
			}))
			defer server.Close()
			defer release.Do(func() { close(releaseResponse) })
			connSvc := newTestSiteConnectionService(db, "test-key", t.TempDir())
			conn, err := connSvc.Create(siteconnectionapp.CreateInput{Name: "poll-callback-race", BaseURL: server.URL, ApiKey: "key", ApiSecret: "secret", Protocol: constants.ConnectionProtocolDujiaoNext})
			if err != nil {
				t.Fatal(err)
			}
			proc := createTestProcurementOrder(t, db, conn.ID, order.ID, order.OrderNo, constants.ProcurementStatusAccepted)
			if err := db.Model(proc).Update("upstream_order_id", 123).Error; err != nil {
				t.Fatal(err)
			}
			queue := &countingPollQueue{}
			svc := procurementapp.NewService(procurementapp.Options{
				Repository: procurementgormstore.New(db), Connections: procurementupstream.New(connSvc), Queue: queue,
				OrderLifecycle: procurementgormstore.NewLifecycle(db, nil, nil, config.EmailConfig{}),
			})
			pollResult := make(chan error, 1)
			go func() { pollResult <- svc.PollUpstreamStatus(proc.ID) }()
			select {
			case <-queryStarted:
			case err := <-pollResult:
				t.Fatalf("poll stopped before HTTP query: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("poll did not reach HTTP query")
			}
			if err := svc.HandleUpstreamCallback(proc.ID, tc.callback, &procurementcontract.Fulfillment{Payload: "RACE-TEST-CARD"}); err != nil {
				t.Fatal(err)
			}
			release.Do(func() { close(releaseResponse) })
			if err := <-pollResult; err != nil {
				t.Fatalf("stale poll response: %v", err)
			}
			var stored ProcurementOrder
			if err := db.First(&stored, proc.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Status != tc.want || stored.RetryCount != 0 || queue.polls != 0 {
				t.Fatalf("stale poll overwrote callback: status=%s retry_count=%d enqueued=%d", stored.Status, stored.RetryCount, queue.polls)
			}
		})
	}
}

func TestPollUpstreamStatus_FulfilledMappedToDelivered(t *testing.T) {
	db := setupProcurementTestDB(t)

	order := createProcTestOrder(t, db, "PROC-POLL-FULLFILLED-001", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		now := time.Now()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"order_id": 1001,
			"order_no": "UP-1001",
			"status":   "fulfilled",
			"amount":   "50.00",
			"currency": "CNY",
			"fulfillment": map[string]any{
				"type":         "auto",
				"status":       "delivered",
				"payload":      "KEY-003\nKEY-004",
				"delivered_at": now.Format(time.RFC3339),
			},
		})
	}))
	defer server.Close()

	connSvc := newTestSiteConnectionService(db, "test-key", t.TempDir())
	conn, _ := connSvc.Create(siteconnectionapp.CreateInput{
		Name: "poll-upstream-fulfilled", BaseURL: server.URL,
		ApiKey: "key", ApiSecret: "secret", Protocol: constants.ConnectionProtocolDujiaoNext,
	})

	proc := createTestProcurementOrder(t, db, conn.ID, order.ID, order.OrderNo, "accepted")
	db.Model(proc).Updates(map[string]interface{}{
		"upstream_order_id": uint(1001),
		"upstream_order_no": "UP-1001",
	})

	svc := newTestProcurementService(db, connSvc)
	if err := svc.PollUpstreamStatus(proc.ID); err != nil {
		t.Fatalf("PollUpstreamStatus: %v", err)
	}

	var updatedProc ProcurementOrder
	db.First(&updatedProc, proc.ID)
	if updatedProc.Status != "fulfilled" {
		t.Errorf("expected procurement status 'fulfilled', got %q", updatedProc.Status)
	}

	var updatedOrder orderdomain.Order
	db.First(&updatedOrder, order.ID)
	if updatedOrder.Status != constants.OrderStatusDelivered {
		t.Errorf("expected order status %q, got %q", constants.OrderStatusDelivered, updatedOrder.Status)
	}
}
