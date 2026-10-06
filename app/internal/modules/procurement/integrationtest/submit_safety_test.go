// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package procurement_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	procurementapp "github.com/dujiao-next/internal/modules/procurement/application"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
	siteconnectionapp "github.com/dujiao-next/internal/modules/siteconnection/application"
	"gorm.io/gorm"
)

func newSubmissionSafetyFixture(t *testing.T, handler http.HandlerFunc) (*gorm.DB, *procurementapp.Service, *ProcurementOrder, *orderdomain.Order) {
	t.Helper()
	db := setupProcurementTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	connSvc := newTestSiteConnectionService(db, "test-key", t.TempDir())
	conn, err := connSvc.Create(siteconnectionapp.CreateInput{
		Name: "submission-safety-upstream", BaseURL: server.URL,
		ApiKey: "test-key", ApiSecret: "test-secret", Protocol: constants.ConnectionProtocolDujiaoNext,
	})
	if err != nil {
		t.Fatal(err)
	}
	order := createProcTestOrder(t, db, "SUBMISSION-SAFETY-001", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
	pm := &mappingdomain.Mapping{ConnectionID: conn.ID, LocalProductID: 1, UpstreamProductID: 101, IsActive: true}
	if err := db.Create(pm).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&mappingdomain.SKUMapping{ProductMappingID: pm.ID, LocalSKUID: 1, UpstreamSKUID: 201, UpstreamIsActive: true}).Error; err != nil {
		t.Fatal(err)
	}
	proc := createTestProcurementOrder(t, db, conn.ID, order.ID, order.OrderNo, "pending")
	return db, newTestProcurementService(db, connSvc), proc, order
}

func writeAcceptedSubmission(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "order_id": 999, "order_no": "UP-999", "status": "paid", "amount": "50.00", "currency": "CNY"})
}

func TestSubmitSafety_ConcurrentWorkersOnlyPurchaseOnce(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	db, svc, proc, _ := newSubmissionSafetyFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		close(entered)
		<-release
		writeAcceptedSubmission(w)
	})
	first := make(chan error, 1)
	go func() { first <- svc.SubmitToUpstream(proc.ID) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("first purchase request did not start")
	}
	secondErr := svc.SubmitToUpstream(proc.ID)
	retryErr := svc.RetryManual(proc.ID)
	cancelErr := svc.CancelManual(proc.ID)
	close(release)
	if err := <-first; err != nil || secondErr != nil {
		t.Fatalf("submit errors: first=%v second=%v", err, secondErr)
	}
	if !errors.Is(retryErr, procurementcontract.ErrStatusInvalid) || !errors.Is(cancelErr, procurementcontract.ErrStatusInvalid) {
		t.Fatalf("in-flight manual operations must be blocked: retry=%v cancel=%v", retryErr, cancelErr)
	}
	if requests.Load() != 1 {
		t.Fatalf("expected one upstream purchase, got %d", requests.Load())
	}
	var stored ProcurementOrder
	db.First(&stored, proc.ID)
	if stored.Status != "accepted" {
		t.Fatalf("expected accepted, got %s", stored.Status)
	}
}

func TestSubmitSafety_LostResponseBlocksRepurchase(t *testing.T) {
	var requests atomic.Int32
	db, svc, proc, _ := newSubmissionSafetyFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1) // simulate upstream charge followed by a broken connection
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})
	if err := svc.SubmitToUpstream(proc.ID); err != nil {
		t.Fatal(err)
	}
	var stored ProcurementOrder
	db.First(&stored, proc.ID)
	if stored.Status != procurementdomain.StatusUnknown || stored.NextRetryAt != nil {
		t.Fatalf("unknown response must stop automatic retry: status=%s next=%v", stored.Status, stored.NextRetryAt)
	}
	if err := svc.SubmitToUpstream(proc.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(svc.RetryManual(proc.ID), procurementcontract.ErrStatusInvalid) || !errors.Is(svc.CancelManual(proc.ID), procurementcontract.ErrStatusInvalid) {
		t.Fatal("unknown purchase must block manual retry and local cancel")
	}
	if requests.Load() != 1 {
		t.Fatalf("lost response caused repeated purchase: %d", requests.Load())
	}
}

func TestSubmitSafety_InvalidSuccessAndAmbiguousErrorsRequireReconciliation(t *testing.T) {
	for name, response := range map[string]map[string]any{
		"missing-id":         {"ok": true, "status": "paid"},
		"missing-status":     {"ok": true, "order_id": 1},
		"unpaid":             {"ok": true, "order_id": 1, "status": "pending_payment"},
		"canceled":           {"ok": true, "order_id": 1, "status": "canceled"},
		"server-error":       {"ok": false, "error_code": "server_error"},
		"duplicate":          {"ok": false, "error_code": "duplicate_order"},
		"failure-with-order": {"ok": false, "order_id": 1, "error_code": "rate_limited"},
	} {
		t.Run(name, func(t *testing.T) {
			db, svc, proc, _ := newSubmissionSafetyFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(response)
			})
			if err := svc.SubmitToUpstream(proc.ID); err != nil {
				t.Fatal(err)
			}
			var stored ProcurementOrder
			db.First(&stored, proc.ID)
			if stored.Status != procurementdomain.StatusUnknown {
				t.Fatalf("expected unknown, got %s", stored.Status)
			}
		})
	}
}

func TestSubmitSafety_FastDeliveryCallbackKeepsTerminalState(t *testing.T) {
	var svc *procurementapp.Service
	var proc *ProcurementOrder
	var callbackErr error
	db, initialized, initializedProc, order := newSubmissionSafetyFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		callbackErr = svc.HandleUpstreamCallback(proc.ID, "delivered", &procurementcontract.Fulfillment{Type: "upstream", Status: "delivered", Payload: "TEST-CARD-ONLY"})
		writeAcceptedSubmission(w)
	})
	svc, proc = initialized, initializedProc
	if err := svc.SubmitToUpstream(proc.ID); err != nil {
		t.Fatal(err)
	}
	if callbackErr != nil {
		t.Fatalf("fast callback failed: %v", callbackErr)
	}
	var stored ProcurementOrder
	db.First(&stored, proc.ID)
	var local orderdomain.Order
	db.First(&local, order.ID)
	if stored.Status != "fulfilled" || local.Status != constants.OrderStatusDelivered || stored.UpstreamOrderID != 999 {
		t.Fatalf("response regressed terminal callback: procurement=%s local=%s upstream-id=%d", stored.Status, local.Status, stored.UpstreamOrderID)
	}
}

func TestSubmitSafety_RefundedLocalOrderCannotPurchase(t *testing.T) {
	var requests atomic.Int32
	db, svc, proc, order := newSubmissionSafetyFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeAcceptedSubmission(w)
	})
	db.Model(order).Updates(map[string]any{"status": constants.OrderStatusRefunded, "refunded_amount": "100.00"})
	if !errors.Is(svc.SubmitToUpstream(proc.ID), procurementcontract.ErrStatusInvalid) {
		t.Fatal("refunded local order should block purchase")
	}
	if requests.Load() != 0 {
		t.Fatal("refunded order triggered upstream purchase")
	}
}

func TestSubmitSafety_DelayedRetryCannotPurchaseEarly(t *testing.T) {
	var requests atomic.Int32
	db, svc, proc, _ := newSubmissionSafetyFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeAcceptedSubmission(w)
	})
	db.Model(proc).Updates(map[string]any{"status": "failed", "next_retry_at": time.Now().Add(time.Hour)})
	if err := svc.SubmitToUpstream(proc.ID); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("duplicate early queue job bypassed next_retry_at")
	}
}

func TestSubmitSafety_RefundHistoryAndParentRefundBlockClaim(t *testing.T) {
	for _, scenario := range []string{"local-refund-record", "parent-refund-record", "parent-refunded"} {
		t.Run(scenario, func(t *testing.T) {
			var requests atomic.Int32
			db, svc, proc, order := newSubmissionSafetyFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				writeAcceptedSubmission(w)
			})
			refundOrderID := order.ID
			if scenario != "local-refund-record" {
				parent := &orderdomain.Order{OrderNo: "REFUNDED-PARENT-FIXTURE", Currency: "CNY", Status: constants.OrderStatusPaid}
				if err := db.Create(parent).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(order).Update("parent_id", parent.ID).Error; err != nil {
					t.Fatal(err)
				}
				refundOrderID = parent.ID
				if scenario == "parent-refunded" {
					db.Model(parent).Updates(map[string]any{"status": constants.OrderStatusRefunded, "refunded_amount": "100.00"})
				}
			}
			if scenario != "parent-refunded" {
				deletedAt := time.Now()
				// Even a hidden historic refund forbids a fresh automatic purchase.
				if err := db.Create(&orderdomain.OrderRefundRecord{OrderID: refundOrderID, Type: "refund", Currency: "CNY", DeletedAt: &deletedAt}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := svc.SubmitToUpstream(proc.ID); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 0 {
				t.Fatal("refund history or refunded parent failed to block purchase claim")
			}
		})
	}
}

func TestSubmitSafety_FailureResponseCannotUndoDeliveredCallback(t *testing.T) {
	var svc *procurementapp.Service
	var proc *ProcurementOrder
	callbackResult := make(chan error, 1)
	db, initialized, initializedProc, order := newSubmissionSafetyFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		callbackResult <- svc.HandleUpstreamCallback(proc.ID, "delivered", &procurementcontract.Fulfillment{Type: "upstream", Status: "delivered", Payload: "TEST-CARD-ONLY"})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": "product_out_of_stock"})
	})
	svc, proc = initialized, initializedProc
	if err := svc.SubmitToUpstream(proc.ID); err != nil {
		t.Fatal(err)
	}
	if err := <-callbackResult; err != nil {
		t.Fatal(err)
	}
	var stored ProcurementOrder
	db.First(&stored, proc.ID)
	var local orderdomain.Order
	db.First(&local, order.ID)
	if stored.Status != "fulfilled" || local.Status != constants.OrderStatusDelivered {
		t.Fatalf("failure response undid callback: procurement=%s local=%s", stored.Status, local.Status)
	}
}

func TestSubmitSafety_CancelFailureLeavesAcceptedOrder(t *testing.T) {
	db, svc, proc, _ := newSubmissionSafetyFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	db.Model(proc).Updates(map[string]any{"status": "accepted", "upstream_order_id": 999})
	if err := svc.CancelManual(proc.ID); err == nil {
		t.Fatal("failed upstream cancellation must be reported")
	}
	var stored ProcurementOrder
	db.First(&stored, proc.ID)
	if stored.Status != "accepted" {
		t.Fatalf("unconfirmed upstream cancellation changed local status to %s", stored.Status)
	}
}
