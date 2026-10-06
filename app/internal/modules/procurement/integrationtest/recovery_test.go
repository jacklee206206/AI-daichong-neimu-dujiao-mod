// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package procurement_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	mappingdomain "github.com/dujiao-next/internal/modules/catalog/mapping/domain"
	mappinggormstore "github.com/dujiao-next/internal/modules/catalog/mapping/infrastructure/gormstore"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	ordergormstore "github.com/dujiao-next/internal/modules/order/infrastructure/gormstore"
	procurementapp "github.com/dujiao-next/internal/modules/procurement/application"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
	procurementgormstore "github.com/dujiao-next/internal/modules/procurement/infrastructure/gormstore"
	procurementmapping "github.com/dujiao-next/internal/modules/procurement/infrastructure/mappingreader"
	procurementorder "github.com/dujiao-next/internal/modules/procurement/infrastructure/orderreader"
	siteconnectiondomain "github.com/dujiao-next/internal/modules/siteconnection/domain"
	"gorm.io/gorm"
)

type recoveryQueue struct {
	ids []uint
	err error
}

func createRecoveryActiveMapping(t *testing.T, db *gorm.DB) {
	t.Helper()
	connection := &siteconnectiondomain.Connection{Name: "test connection", BaseURL: "https://example.invalid", ApiKey: "test", ApiSecret: "test", Status: constants.ConnectionStatusActive}
	if err := db.Create(connection).Error; err != nil {
		t.Fatal(err)
	}
	mapping := &mappingdomain.Mapping{ConnectionID: connection.ID, LocalProductID: 1, UpstreamProductID: 101, IsActive: true, UpstreamStatus: "active"}
	if err := db.Create(mapping).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&mappingdomain.SKUMapping{ProductMappingID: mapping.ID, LocalSKUID: 1, UpstreamSKUID: 201, UpstreamIsActive: true}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestMissingProcurementRecoveryUsesPaidLeafUpstreamSnapshotOnly(t *testing.T) {
	cases := []struct {
		name    string
		allowed bool
		change  func(*testing.T, *gorm.DB, *orderdomain.Order)
	}{
		{name: "paid upstream", allowed: true},
		{name: "fulfilling upstream", allowed: true, change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE orders SET status = ? WHERE id = ?", constants.OrderStatusFulfilling, o.ID)
		}},
		{name: "paid leaf child", allowed: true, change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			parent := createProcTestOrder(t, db, "PAID-PARENT", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
			mustRecoveryExec(t, db, "UPDATE orders SET parent_id = ? WHERE id = ?", parent.ID, o.ID)
		}},
		{name: "historical auto with current mapping", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE order_items SET fulfillment_type = ? WHERE order_id = ?", constants.FulfillmentTypeAuto, o.ID)
		}},
		{name: "historical manual with current mapping", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE order_items SET fulfillment_type = ? WHERE order_id = ?", constants.FulfillmentTypeManual, o.ID)
		}},
		{name: "unpaid", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE orders SET status = ? WHERE id = ?", constants.OrderStatusPendingPayment, o.ID)
		}},
		{name: "refunded status", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE orders SET status = ? WHERE id = ?", constants.OrderStatusRefunded, o.ID)
		}},
		{name: "refunded amount", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE orders SET refunded_amount = 1 WHERE id = ?", o.ID)
		}},
		{name: "refund record", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			if err := db.Create(&orderdomain.OrderRefundRecord{OrderID: o.ID, Type: "manual"}).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "soft deleted refund record", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			now := time.Now()
			if err := db.Create(&orderdomain.OrderRefundRecord{OrderID: o.ID, Type: "manual", DeletedAt: &now}).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "parent order", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			child := createProcTestOrder(t, db, "AUTO-CHILD", constants.OrderStatusPaid, constants.FulfillmentTypeAuto)
			mustRecoveryExec(t, db, "UPDATE orders SET parent_id = ? WHERE id = ?", o.ID, child.ID)
		}},
		{name: "refunded parent", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			parent := createProcTestOrder(t, db, "REFUND-PARENT", constants.OrderStatusRefunded, constants.FulfillmentTypeUpstream)
			mustRecoveryExec(t, db, "UPDATE orders SET parent_id = ? WHERE id = ?", parent.ID, o.ID)
		}},
		{name: "existing unknown", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			createTestProcurementOrder(t, db, 1, o.ID, o.OrderNo, procurementdomain.StatusUnknown)
		}},
		{name: "soft deleted procurement", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			p := createTestProcurementOrder(t, db, 1, o.ID, o.OrderNo, procurementdomain.StatusUnknown)
			mustRecoveryExec(t, db, "UPDATE procurement_orders SET deleted_at = ? WHERE id = ?", time.Now(), p.ID)
		}},
		{name: "existing rejected", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			createTestProcurementOrder(t, db, 1, o.ID, o.OrderNo, "rejected")
		}},
		{name: "existing delivery", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			if err := db.Create(&fulfillmentdomain.Fulfillment{OrderID: o.ID, Type: constants.FulfillmentTypeUpstream, Status: constants.FulfillmentStatusDelivered, Payload: "test-only"}).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "multiple upstream items", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			item := o.Items[0]
			item.ID = 0
			if err := db.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mixed fulfillment snapshot", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			item := o.Items[0]
			item.ID = 0
			item.FulfillmentType = constants.FulfillmentTypeAuto
			if err := db.Create(&item).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "inactive mapping", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE product_mappings SET is_active = ?", false)
		}},
		{name: "deleted mapping", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE product_mappings SET deleted_at = ?", time.Now())
		}},
		{name: "inactive sku mapping", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE sku_mappings SET upstream_is_active = ?", false)
		}},
		{name: "missing sku mapping", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "DELETE FROM sku_mappings")
		}},
		{name: "disabled connection", change: func(t *testing.T, db *gorm.DB, o *orderdomain.Order) {
			mustRecoveryExec(t, db, "UPDATE site_connections SET status = ?", constants.ConnectionStatusDisabled)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupProcurementTestDB(t)
			createRecoveryActiveMapping(t, db)
			o := createProcTestOrder(t, db, "MISSING", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
			if tc.change != nil {
				tc.change(t, db, o)
			}
			var before int64
			if err := db.Model(&procurementdomain.Order{}).Count(&before).Error; err != nil {
				t.Fatal(err)
			}
			q := &recoveryQueue{}
			recoveryTestService(db, q).SyncAcceptedOrders()
			var after int64
			if err := db.Model(&procurementdomain.Order{}).Count(&after).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(0)
			if tc.allowed {
				want = 1
			}
			if after-before != want || int64(len(q.ids)) != want {
				t.Fatalf("created=%d queued=%v; want=%d", after-before, q.ids, want)
			}
		})
	}
}

func mustRecoveryExec(t *testing.T, db *gorm.DB, query string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatal(err)
	}
}

func TestMissingProcurementRecoveryPagesByOrderID(t *testing.T) {
	db := setupProcurementTestDB(t)
	createRecoveryActiveMapping(t, db)
	var expected []uint
	for i := 0; i < 3; i++ {
		o := createProcTestOrder(t, db, fmt.Sprintf("MISSING-PAGE-%d", i), constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
		expected = append(expected, o.ID)
	}
	store := procurementgormstore.New(db)
	var afterID uint
	for _, want := range expected {
		ids, err := store.ListPaidOrdersMissingProcurement(afterID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 1 || ids[0] != want {
			t.Fatalf("page after %d got=%v want=%d", afterID, ids, want)
		}
		afterID = ids[0]
	}
	ids, err := store.ListPaidOrdersMissingProcurement(afterID, 1)
	if err != nil || len(ids) != 0 {
		t.Fatalf("end page: ids=%v err=%v", ids, err)
	}
	q := &recoveryQueue{}
	recoveryTestService(db, q).SyncAcceptedOrders()
	if len(q.ids) != len(expected) {
		t.Fatalf("missing orders not restored: %v", q.ids)
	}
}

func (q *recoveryQueue) EnqueueSubmit(id uint, _ ...time.Duration) error {
	if q.err != nil {
		return q.err
	}
	q.ids = append(q.ids, id)
	return nil
}
func (q *recoveryQueue) EnqueuePoll(uint, time.Duration) error { return nil }

func recoveryTestService(db *gorm.DB, queue *recoveryQueue) *procurementapp.Service {
	return procurementapp.NewService(procurementapp.Options{
		Repository:      procurementgormstore.New(db),
		Orders:          procurementorder.New(ordergormstore.New(db, "test-guest-credential-secret-with-32-bytes")),
		ProductMappings: procurementmapping.NewProducts(mappinggormstore.NewMappingStore(db)),
		Queue:           queue,
	})
}

func TestCreateForParentContinuesPastExistingChild(t *testing.T) {
	db := setupProcurementTestDB(t)
	parent := createProcTestOrder(t, db, "PARENT", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
	first := createProcTestOrder(t, db, "FIRST", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
	second := createProcTestOrder(t, db, "SECOND", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
	if err := db.Model(&orderdomain.Order{}).Where("id IN ?", []uint{first.ID, second.ID}).Update("parent_id", parent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&mappingdomain.Mapping{ConnectionID: 1, LocalProductID: 1, UpstreamProductID: 101, IsActive: true}).Error; err != nil {
		t.Fatal(err)
	}
	createTestProcurementOrder(t, db, 1, first.ID, first.OrderNo, "accepted")
	q := &recoveryQueue{}
	if err := recoveryTestService(db, q).CreateForOrder(parent.ID); err != nil {
		t.Fatal(err)
	}
	got, err := procurementgormstore.New(db).GetByLocalOrderID(second.ID)
	if err != nil || got == nil {
		t.Fatalf("second child was skipped: order=%v err=%v", got, err)
	}
	if len(q.ids) != 1 || q.ids[0] != got.ID {
		t.Fatalf("unexpected submissions: %v", q.ids)
	}
}

func TestProcurementCreationUniqueUnderConcurrentInserts(t *testing.T) {
	db := setupProcurementTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	store := procurementgormstore.New(db)
	const n = 16
	results := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- store.Create(&procurementdomain.Order{LocalOrderID: 55, ConnectionID: 1, Currency: "CNY"})
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	for err := range results {
		if err == nil {
			created++
		} else if !errors.Is(err, procurementcontract.ErrExists) {
			t.Fatalf("unexpected insert error: %v", err)
		}
	}
	if created != 1 {
		t.Fatalf("expected one purchase row, got %d", created)
	}
	var count int64
	db.Model(&procurementdomain.Order{}).Where("local_order_id = ?", 55).Count(&count)
	if count != 1 {
		t.Fatalf("duplicate purchase rows: %d", count)
	}
}

func TestCreateForParentPersistsOtherChildrenDuringRedisOutage(t *testing.T) {
	db := setupProcurementTestDB(t)
	parent := createProcTestOrder(t, db, "REDIS-PARENT", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
	first := createProcTestOrder(t, db, "REDIS-FIRST", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
	second := createProcTestOrder(t, db, "REDIS-SECOND", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
	if err := db.Model(&orderdomain.Order{}).Where("id IN ?", []uint{first.ID, second.ID}).Update("parent_id", parent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&mappingdomain.Mapping{ConnectionID: 1, LocalProductID: 1, UpstreamProductID: 101, IsActive: true}).Error; err != nil {
		t.Fatal(err)
	}
	q := &recoveryQueue{err: errors.New("redis unavailable")}
	svc := recoveryTestService(db, q)
	if err := svc.CreateForOrder(parent.ID); err == nil {
		t.Fatal("expected enqueue error")
	}
	var count int64
	if err := db.Model(&procurementdomain.Order{}).Where("local_order_id IN ? AND status = ?", []uint{first.ID, second.ID}, procurementdomain.StatusPending).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("one failed enqueue prevented other child recovery: count=%d", count)
	}
	q.err = nil
	svc.SyncAcceptedOrders()
	if len(q.ids) != 2 {
		t.Fatalf("expected both child submissions restored: %v", q.ids)
	}
}

func TestRecoveryRepairsFailedEnqueueWithoutRepeatingUnknownPurchase(t *testing.T) {
	db := setupProcurementTestDB(t)
	order := createProcTestOrder(t, db, "QUEUE-FAIL", constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
	if err := db.Create(&mappingdomain.Mapping{ConnectionID: 1, LocalProductID: 1, UpstreamProductID: 101, IsActive: true}).Error; err != nil {
		t.Fatal(err)
	}
	q := &recoveryQueue{err: errors.New("redis unavailable")}
	svc := recoveryTestService(db, q)
	if err := svc.CreateForOrder(order.ID); err == nil {
		t.Fatal("enqueue failure was hidden")
	}
	pending, err := procurementgormstore.New(db).GetByLocalOrderID(order.ID)
	if err != nil || pending == nil {
		t.Fatalf("durable pending row missing: %v", err)
	}
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	expected := map[uint]bool{pending.ID: true}
	for i, state := range []string{"failed", "failed", "failed", "unknown", "submitting", "rejected", "pending"} {
		local := createProcTestOrder(t, db, fmt.Sprintf("RECOVER-%d", i), constants.OrderStatusPaid, constants.FulfillmentTypeUpstream)
		p := createTestProcurementOrder(t, db, 1, local.ID, local.OrderNo, state)
		updates := map[string]interface{}{}
		switch i {
		case 0:
			updates["next_retry_at"] = past
			expected[p.ID] = true
		case 1:
			updates["next_retry_at"] = future
		case 4:
			updates["updated_at"] = past
		case 6:
			updates["upstream_order_id"] = 99
		}
		if len(updates) > 0 {
			if err := db.Model(p).Updates(updates).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	q.err = nil
	svc.SyncAcceptedOrders()
	if len(q.ids) != len(expected) {
		t.Fatalf("unexpected recovery submissions: %v", q.ids)
	}
	for _, id := range q.ids {
		if !expected[id] {
			t.Fatalf("unsafe purchase was requeued: %d", id)
		}
	}
	var stale procurementdomain.Order
	if err := db.Where("local_order_no = ?", "RECOVER-4").First(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if stale.Status != procurementdomain.StatusUnknown {
		t.Fatalf("stale claim status=%s", stale.Status)
	}
}
