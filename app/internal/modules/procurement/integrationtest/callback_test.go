// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package procurement_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"github.com/dujiao-next/internal/constants"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
)

type procurementCallbackStatusFixture struct {
	orderNo                   string
	initialOrderStatus        string
	initialProcurementStatus  string
	callbackStatus            string
	expectedProcurementStatus string
	expectedOrderStatus       string
}

func assertProcurementCallbackStatus(t *testing.T, fixture procurementCallbackStatusFixture) {
	t.Helper()
	db := setupProcurementTestDB(t)

	order := createProcTestOrder(t, db, fixture.orderNo, fixture.initialOrderStatus, constants.FulfillmentTypeUpstream)
	proc := createTestProcurementOrder(t, db, 1, order.ID, order.OrderNo, fixture.initialProcurementStatus)

	connSvc := newTestSiteConnectionService(db, "test-key", t.TempDir())
	svc := newTestProcurementService(db, connSvc)

	if err := svc.HandleUpstreamCallback(proc.ID, fixture.callbackStatus, nil); err != nil {
		t.Fatalf("HandleUpstreamCallback: %v", err)
	}

	var updatedProc ProcurementOrder
	if err := db.First(&updatedProc, proc.ID).Error; err != nil {
		t.Fatalf("load procurement: %v", err)
	}
	if updatedProc.Status != fixture.expectedProcurementStatus {
		t.Errorf("expected procurement status %q, got %q", fixture.expectedProcurementStatus, updatedProc.Status)
	}

	var updatedOrder orderdomain.Order
	if err := db.First(&updatedOrder, order.ID).Error; err != nil {
		t.Fatalf("load order: %v", err)
	}
	if updatedOrder.Status != fixture.expectedOrderStatus {
		t.Errorf("expected order status %q, got %q", fixture.expectedOrderStatus, updatedOrder.Status)
	}
}

// ── Phase 1 tests: order rollback on procurement failure ──

func TestRejectProcurement_RollsBackOrderStatus(t *testing.T) {
	db := setupProcurementTestDB(t)

	order := createProcTestOrder(t, db, "PROC-REJECT-001", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
	proc := createTestProcurementOrder(t, db, 1, order.ID, order.OrderNo, "pending")

	connSvc := newTestSiteConnectionService(db, "test-key", t.TempDir())
	svc := newTestProcurementService(db, connSvc)

	if err := svc.SubmitToUpstream(proc.ID); err != nil {
		t.Fatalf("SubmitToUpstream with missing connection: %v", err)
	}

	// 验证采购单状态 = rejected
	var updatedProc ProcurementOrder
	if err := db.First(&updatedProc, proc.ID).Error; err != nil {
		t.Fatalf("load procurement: %v", err)
	}
	if updatedProc.Status != "rejected" {
		t.Errorf("expected procurement status 'rejected', got %q", updatedProc.Status)
	}

	// 验证本地订单状态从 fulfilling 回退到 paid
	var updatedOrder orderdomain.Order
	if err := db.First(&updatedOrder, order.ID).Error; err != nil {
		t.Fatalf("load order: %v", err)
	}
	if updatedOrder.Status != constants.OrderStatusPaid {
		t.Errorf("expected order status %q, got %q", constants.OrderStatusPaid, updatedOrder.Status)
	}
}

func TestHandleUpstreamCallback_Canceled_RollsBackOrder(t *testing.T) {
	db := setupProcurementTestDB(t)

	order := createProcTestOrder(t, db, "PROC-CANCEL-001", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
	proc := createTestProcurementOrder(t, db, 1, order.ID, order.OrderNo, "accepted")

	connSvc := newTestSiteConnectionService(db, "test-key", t.TempDir())
	svc := newTestProcurementService(db, connSvc)

	if err := svc.HandleUpstreamCallback(proc.ID, "canceled", nil); err != nil {
		t.Fatalf("HandleUpstreamCallback: %v", err)
	}

	// 验证采购单状态 = canceled
	var updatedProc ProcurementOrder
	if err := db.First(&updatedProc, proc.ID).Error; err != nil {
		t.Fatalf("load procurement: %v", err)
	}
	if updatedProc.Status != "canceled" {
		t.Errorf("expected procurement status 'canceled', got %q", updatedProc.Status)
	}

	// 验证本地订单状态从 fulfilling 回退到 paid
	var updatedOrder orderdomain.Order
	if err := db.First(&updatedOrder, order.ID).Error; err != nil {
		t.Fatalf("load order: %v", err)
	}
	if updatedOrder.Status != constants.OrderStatusPaid {
		t.Errorf("expected order status %q, got %q", constants.OrderStatusPaid, updatedOrder.Status)
	}
}

func TestHandleUpstreamCallback_Delivered_CreatesFulfillment(t *testing.T) {
	db := setupProcurementTestDB(t)

	order := createProcTestOrder(t, db, "PROC-DELIVER-001", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
	proc := createTestProcurementOrder(t, db, 1, order.ID, order.OrderNo, "accepted")

	connSvc := newTestSiteConnectionService(db, "test-key", t.TempDir())
	svc := newTestProcurementService(db, connSvc)

	now := time.Now()
	fulfillment := &procurementcontract.Fulfillment{
		Type:        constants.FulfillmentTypeUpstream,
		Status:      constants.FulfillmentStatusDelivered,
		Payload:     "CDK-001\nCDK-002",
		DeliveredAt: &now,
	}

	if err := svc.HandleUpstreamCallback(proc.ID, "delivered", fulfillment); err != nil {
		t.Fatalf("HandleUpstreamCallback: %v", err)
	}

	// 验证采购单状态 = fulfilled
	var updatedProc ProcurementOrder
	if err := db.First(&updatedProc, proc.ID).Error; err != nil {
		t.Fatalf("load procurement: %v", err)
	}
	if updatedProc.Status != "fulfilled" {
		t.Errorf("expected procurement status 'fulfilled', got %q", updatedProc.Status)
	}

	// 验证本地订单状态 = delivered
	var updatedOrder orderdomain.Order
	if err := db.First(&updatedOrder, order.ID).Error; err != nil {
		t.Fatalf("load order: %v", err)
	}
	if updatedOrder.Status != constants.OrderStatusDelivered {
		t.Errorf("expected order status %q, got %q", constants.OrderStatusDelivered, updatedOrder.Status)
	}

	// 验证 Fulfillment 记录已创建
	var ff fulfillmentdomain.Fulfillment
	if err := db.Where("order_id = ?", order.ID).First(&ff).Error; err != nil {
		t.Fatalf("expected fulfillment record to exist: %v", err)
	}
	if ff.Payload != "CDK-001\nCDK-002" {
		t.Errorf("unexpected fulfillment payload: %q", ff.Payload)
	}
	if ff.Type != constants.FulfillmentTypeUpstream {
		t.Errorf("expected fulfillment type %q, got %q", constants.FulfillmentTypeUpstream, ff.Type)
	}
}

func TestHandleUpstreamCallback_Delivered_SynchronizesParentStatus(t *testing.T) {
	db := setupProcurementTestDB(t)
	parent := createProcTestOrder(t, db, "PROC-PARENT-DELIVERED", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
	child := createProcTestOrder(t, db, "PROC-CHILD-DELIVERED", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
	if err := db.Model(&child).Update("parent_id", parent.ID).Error; err != nil {
		t.Fatalf("set child parent: %v", err)
	}
	proc := createTestProcurementOrder(t, db, 1, child.ID, child.OrderNo, constants.ProcurementStatusAccepted)

	svc := newTestProcurementService(db, newTestSiteConnectionService(db, "test-key", t.TempDir()))
	if err := svc.HandleUpstreamCallback(proc.ID, "delivered", &procurementcontract.Fulfillment{
		Type: constants.FulfillmentTypeAuto, Status: constants.FulfillmentStatusDelivered, Payload: "PARENT-TEST-CARD",
	}); err != nil {
		t.Fatalf("HandleUpstreamCallback: %v", err)
	}

	var updatedParent orderdomain.Order
	if err := db.First(&updatedParent, parent.ID).Error; err != nil {
		t.Fatalf("load parent order: %v", err)
	}
	if updatedParent.Status != constants.OrderStatusDelivered {
		t.Fatalf("parent status = %q, want %q", updatedParent.Status, constants.OrderStatusDelivered)
	}
}

func TestHandleUpstreamCallback_PartiallyRefunded_AfterFulfilledUpdatesProcurementStatus(t *testing.T) {
	assertProcurementCallbackStatus(t, procurementCallbackStatusFixture{
		orderNo:                   "PROC-REFUND-KEEP-001",
		initialOrderStatus:        constants.OrderStatusDelivered,
		initialProcurementStatus:  constants.ProcurementStatusFulfilled,
		callbackStatus:            "partially_refunded",
		expectedProcurementStatus: constants.ProcurementStatusPartiallyRefunded,
		expectedOrderStatus:       constants.OrderStatusDelivered,
	})
}

func TestHandleUpstreamCallback_PartiallyRefunded_WhileFulfillingKeepsOrderStatus(t *testing.T) {
	assertProcurementCallbackStatus(t, procurementCallbackStatusFixture{
		orderNo:                   "PROC-REFUND-FULFILLING-001",
		initialOrderStatus:        constants.OrderStatusFulfilling,
		initialProcurementStatus:  constants.ProcurementStatusAccepted,
		callbackStatus:            "partially_refunded",
		expectedProcurementStatus: constants.ProcurementStatusPartiallyRefunded,
		expectedOrderStatus:       constants.OrderStatusFulfilling,
	})
}

func TestHandleUpstreamCallback_Refunded_AfterCompletedKeepsOrderStatus(t *testing.T) {
	assertProcurementCallbackStatus(t, procurementCallbackStatusFixture{
		orderNo:                   "PROC-REFUND-COMPLETED-001",
		initialOrderStatus:        constants.OrderStatusCompleted,
		initialProcurementStatus:  constants.ProcurementStatusFulfilled,
		callbackStatus:            "refunded",
		expectedProcurementStatus: constants.ProcurementStatusRefunded,
		expectedOrderStatus:       constants.OrderStatusCompleted,
	})
}

func TestHandleUpstreamCallback_EmptyDeliveryRemainsRetryable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fulfillment *procurementcontract.Fulfillment
	}{
		{name: "nil"},
		{name: "empty", fulfillment: &procurementcontract.Fulfillment{}},
		{name: "whitespace", fulfillment: &procurementcontract.Fulfillment{Payload: " \t\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupProcurementTestDB(t)
			order := createProcTestOrder(t, db, "EMPTY-CARD", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
			proc := createTestProcurementOrder(t, db, 1, order.ID, order.OrderNo, constants.ProcurementStatusAccepted)
			svc := newTestProcurementService(db, newTestSiteConnectionService(db, "test-key", t.TempDir()))
			if err := svc.HandleUpstreamCallback(proc.ID, "delivered", tc.fulfillment); err == nil {
				t.Fatal("empty fulfillment must reject the callback")
			}
			var storedOrder orderdomain.Order
			var storedProc ProcurementOrder
			db.First(&storedOrder, order.ID)
			db.First(&storedProc, proc.ID)
			var count int64
			db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", order.ID).Count(&count)
			if storedOrder.Status != constants.OrderStatusFulfilling || storedProc.Status != constants.ProcurementStatusAccepted || count != 0 {
				t.Fatalf("empty callback changed state: order=%s procurement=%s fulfillments=%d", storedOrder.Status, storedProc.Status, count)
			}
			if err := svc.HandleUpstreamCallback(proc.ID, "delivered", &procurementcontract.Fulfillment{Payload: "RECOVERED-CARD"}); err != nil {
				t.Fatalf("valid retry must remain possible: %v", err)
			}
		})
	}
}

func TestHandleUpstreamCallback_DeliveryRollsBackEveryWrite(t *testing.T) {
	for _, failure := range []string{"fulfillment", "procurement", "child", "parent"} {
		t.Run(failure, func(t *testing.T) {
			db := setupProcurementTestDB(t)
			parent := createProcTestOrder(t, db, "TX-PARENT", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
			child := createProcTestOrder(t, db, "TX-CHILD", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
			if err := db.Model(child).Update("parent_id", parent.ID).Error; err != nil {
				t.Fatal(err)
			}
			proc := createTestProcurementOrder(t, db, 1, child.ID, child.OrderNo, constants.ProcurementStatusAccepted)
			table, operation, predicate := "fulfillments", "INSERT", fmt.Sprintf("NEW.order_id = %d", child.ID)
			switch failure {
			case "procurement":
				table, operation, predicate = "procurement_orders", "UPDATE", fmt.Sprintf("NEW.id = %d", proc.ID)
			case "child":
				table, operation, predicate = "orders", "UPDATE", fmt.Sprintf("NEW.id = %d", child.ID)
			case "parent":
				table, operation, predicate = "orders", "UPDATE", fmt.Sprintf("NEW.id = %d", parent.ID)
			}
			trigger := fmt.Sprintf("CREATE TRIGGER reject_callback_write BEFORE %s ON %s WHEN %s BEGIN SELECT RAISE(ABORT, 'injected callback write failure'); END", operation, table, predicate)
			if err := db.Exec(trigger).Error; err != nil {
				t.Fatal(err)
			}
			svc := newTestProcurementService(db, newTestSiteConnectionService(db, "test-key", t.TempDir()))
			fulfillment := &procurementcontract.Fulfillment{Payload: "TRANSACTION-CARD"}
			if err := svc.HandleUpstreamCallback(proc.ID, "delivered", fulfillment); err == nil {
				t.Fatal("injected persistence failure must be returned")
			}
			var storedProc ProcurementOrder
			if err := db.First(&storedProc, proc.ID).Error; err != nil {
				t.Fatal(err)
			}
			if storedProc.Status != constants.ProcurementStatusAccepted || storedProc.UpstreamPayload != "" {
				t.Fatalf("procurement update escaped rollback: %+v", storedProc)
			}
			for _, id := range []uint{parent.ID, child.ID} {
				var order orderdomain.Order
				if err := db.First(&order, id).Error; err != nil {
					t.Fatal(err)
				}
				if order.Status != constants.OrderStatusFulfilling {
					t.Fatalf("order %d escaped rollback: %s", id, order.Status)
				}
			}
			var count int64
			if err := db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", child.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("fulfillment escaped rollback: count=%d", count)
			}
			if err := db.Exec("DROP TRIGGER reject_callback_write").Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.HandleUpstreamCallback(proc.ID, "delivered", fulfillment); err != nil {
				t.Fatalf("retry after storage recovery: %v", err)
			}
			if err := svc.HandleUpstreamCallback(proc.ID, "delivered", fulfillment); err != nil {
				t.Fatalf("duplicate delivery: %v", err)
			}
			if err := db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", child.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("want one fulfillment after retries, got %d", count)
			}
		})
	}
}

func TestHandleUpstreamCallback_ProtectsRefundedAndClosedStates(t *testing.T) {
	for _, tc := range []struct{ name, procurement, order, callback string }{
		{"full_refund_cannot_become_partial", constants.ProcurementStatusRefunded, constants.OrderStatusDelivered, "partially_refunded"},
		{"partial_refund_cannot_become_canceled", constants.ProcurementStatusPartiallyRefunded, constants.OrderStatusFulfilling, "canceled"},
		{"canceled_procurement", constants.ProcurementStatusCanceled, constants.OrderStatusPaid, "delivered"},
		{"refunded_order", constants.ProcurementStatusAccepted, constants.OrderStatusRefunded, "delivered"},
		{"partial_refunded_order", constants.ProcurementStatusAccepted, constants.OrderStatusPartiallyRefunded, "delivered"},
		{"completed_order", constants.ProcurementStatusAccepted, constants.OrderStatusCompleted, "delivered"},
		{"canceled_order", constants.ProcurementStatusAccepted, constants.OrderStatusCanceled, "delivered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupProcurementTestDB(t)
			order := createProcTestOrder(t, db, "TERMINAL-ORDER", tc.order, constants.FulfillmentTypeUpstream)
			proc := createTestProcurementOrder(t, db, 1, order.ID, order.OrderNo, tc.procurement)
			svc := newTestProcurementService(db, newTestSiteConnectionService(db, "test-key", t.TempDir()))
			if err := svc.HandleUpstreamCallback(proc.ID, tc.callback, &procurementcontract.Fulfillment{Payload: "LATE-CARD"}); err != nil {
				t.Fatal(err)
			}
			var storedProc ProcurementOrder
			var storedOrder orderdomain.Order
			db.First(&storedProc, proc.ID)
			db.First(&storedOrder, order.ID)
			if storedProc.Status != tc.procurement || storedOrder.Status != tc.order {
				t.Fatalf("late callback changed terminal states: %s, %s", storedProc.Status, storedOrder.Status)
			}
			var count int64
			db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", order.ID).Count(&count)
			if count != 0 {
				t.Fatalf("late callback created %d fulfillments", count)
			}
		})
	}
}

func TestHandleUpstreamCallback_ConcurrentDeliveryIsIdempotent(t *testing.T) {
	db := setupProcurementTestDB(t)
	order := createProcTestOrder(t, db, "CONCURRENT-CALLBACK", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
	proc := createTestProcurementOrder(t, db, 1, order.ID, order.OrderNo, constants.ProcurementStatusAccepted)
	svc := newTestProcurementService(db, newTestSiteConnectionService(db, "test-key", t.TempDir()))
	fulfillment := &procurementcontract.Fulfillment{Payload: "ONE-CARD"}
	start := make(chan struct{})
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errors <- svc.HandleUpstreamCallback(proc.ID, "delivered", fulfillment)
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	// SQLite may reject concurrent write transactions; the callback must be safe to retry.
	for err := range errors {
		if err != nil {
			if retryErr := svc.HandleUpstreamCallback(proc.ID, "delivered", fulfillment); retryErr != nil {
				t.Fatalf("callback retry failed: %v", retryErr)
			}
		}
	}
	var count int64
	if err := db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one fulfillment, got %d", count)
	}
	var storedProc ProcurementOrder
	var storedOrder orderdomain.Order
	db.First(&storedProc, proc.ID)
	db.First(&storedOrder, order.ID)
	if storedProc.Status != constants.ProcurementStatusFulfilled || storedOrder.Status != constants.OrderStatusDelivered {
		t.Fatalf("incomplete delivery: %s, %s", storedProc.Status, storedOrder.Status)
	}
}

func TestHandleUpstreamCallback_EarlyDeliveryAndUnknownPurchaseCanComplete(t *testing.T) {
	for _, status := range []string{"pending", "submitting", "submitted", "unknown", "failed"} {
		t.Run(status, func(t *testing.T) {
			db := setupProcurementTestDB(t)
			order := createProcTestOrder(t, db, "EARLY-DELIVERY", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
			proc := createTestProcurementOrder(t, db, 1, order.ID, order.OrderNo, status)
			svc := newTestProcurementService(db, newTestSiteConnectionService(db, "test-key", t.TempDir()))
			if err := svc.HandleUpstreamCallback(proc.ID, "delivered", &procurementcontract.Fulfillment{Payload: "EARLY-CARD"}); err != nil {
				t.Fatalf("valid delivery in %s: %v", status, err)
			}
			var storedProc ProcurementOrder
			var storedOrder orderdomain.Order
			db.First(&storedProc, proc.ID)
			db.First(&storedOrder, order.ID)
			if storedProc.Status != constants.ProcurementStatusFulfilled || storedOrder.Status != constants.OrderStatusDelivered {
				t.Fatalf("valid delivery did not complete: %s, %s", storedProc.Status, storedOrder.Status)
			}
		})
	}
}

func TestHandleUpstreamCallback_DoesNotReopenClosedParent(t *testing.T) {
	for _, status := range []string{constants.OrderStatusCanceled, constants.OrderStatusRefunded, constants.OrderStatusCompleted} {
		t.Run(status, func(t *testing.T) {
			db := setupProcurementTestDB(t)
			parent := createProcTestOrder(t, db, "CLOSED-PARENT", status, constants.FulfillmentTypeUpstream)
			child := createProcTestOrder(t, db, "CLOSED-PARENT-CHILD", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
			if err := db.Model(child).Update("parent_id", parent.ID).Error; err != nil {
				t.Fatal(err)
			}
			proc := createTestProcurementOrder(t, db, 1, child.ID, child.OrderNo, constants.ProcurementStatusAccepted)
			svc := newTestProcurementService(db, newTestSiteConnectionService(db, "test-key", t.TempDir()))
			if err := svc.HandleUpstreamCallback(proc.ID, "delivered", &procurementcontract.Fulfillment{Payload: "TOO-LATE-CARD"}); err != nil {
				t.Fatal(err)
			}
			var storedParent, storedChild orderdomain.Order
			var storedProc ProcurementOrder
			db.First(&storedParent, parent.ID)
			db.First(&storedChild, child.ID)
			db.First(&storedProc, proc.ID)
			if storedParent.Status != status || storedChild.Status != constants.OrderStatusFulfilling || storedProc.Status != constants.ProcurementStatusAccepted {
				t.Fatalf("closed parent changed state: %s, %s, %s", storedParent.Status, storedChild.Status, storedProc.Status)
			}
			var count int64
			db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", child.ID).Count(&count)
			if count != 0 {
				t.Fatalf("late callback wrote %d fulfillments", count)
			}
		})
	}
}

func TestHandleUpstreamCallback_DeliveryPreservesPriorPartialRefund(t *testing.T) {
	db := setupProcurementTestDB(t)
	order := createProcTestOrder(t, db, "PARTIAL-REFUND-BEFORE-DELIVERY", constants.OrderStatusFulfilling, constants.FulfillmentTypeUpstream)
	proc := createTestProcurementOrder(t, db, 1, order.ID, order.OrderNo, constants.ProcurementStatusPartiallyRefunded)
	svc := newTestProcurementService(db, newTestSiteConnectionService(db, "test-key", t.TempDir()))
	fulfillment := &procurementcontract.Fulfillment{Payload: "PARTIALLY-REFUNDED-CARD"}
	for i := 0; i < 2; i++ {
		if err := svc.HandleUpstreamCallback(proc.ID, "delivered", fulfillment); err != nil {
			t.Fatal(err)
		}
	}
	var storedProc ProcurementOrder
	var storedOrder orderdomain.Order
	db.First(&storedProc, proc.ID)
	db.First(&storedOrder, order.ID)
	if storedProc.Status != constants.ProcurementStatusPartiallyRefunded || storedOrder.Status != constants.OrderStatusDelivered {
		t.Fatalf("delivery erased partial refund: %s, %s", storedProc.Status, storedOrder.Status)
	}
	var count int64
	db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", order.ID).Count(&count)
	if count != 1 {
		t.Fatalf("expected one fulfillment, got %d", count)
	}
	if err := svc.HandleUpstreamCallback(proc.ID, "refunded", &procurementcontract.Fulfillment{}); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleUpstreamCallback(proc.ID, "partially_refunded", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleUpstreamCallback(proc.ID, "delivered", fulfillment); err != nil {
		t.Fatal(err)
	}
	db.First(&storedProc, proc.ID)
	if storedProc.Status != constants.ProcurementStatusRefunded || storedProc.UpstreamPayload != fulfillment.Payload {
		t.Fatalf("refund callbacks erased final status or delivery evidence: status=%s payload=%q", storedProc.Status, storedProc.UpstreamPayload)
	}
}
