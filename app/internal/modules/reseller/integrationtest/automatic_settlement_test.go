// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	resellerapp "github.com/dujiao-next/internal/modules/reseller/application"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	resellergormstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func seedAutomaticProfit(t *testing.T) (*gorm.DB, *resellergormstore.Store, *resellerapp.AccountingLedgerService, orderdomain.Order, resellerdomain.LedgerEntry, time.Time) {
	t.Helper()
	db, repo, _, order, row := seedManualProfit(t)
	svc := resellerapp.NewAccountingLedgerService(repo, 0)
	completed := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	order.Status = constants.OrderStatusCompleted
	if err := db.Save(&order).Error; err != nil {
		t.Fatal(err)
	}
	seedDelivery(t, db, order.ID, completed)
	return db, repo, svc, order, row, completed
}

func TestAutomaticSettlementExact92HourBoundaryAndLegacyManualIdempotency(t *testing.T) {
	db, _, svc, _, row, completed := seedAutomaticProfit(t)
	// Existing manual entries (and their previous 96h deadline) must adopt the
	// new delivery-based policy, without editing already released historical rows.
	old := completed.Add(96 * time.Hour)
	if err := db.Model(&row).Updates(map[string]interface{}{"available_at": old, "delivery_completed_at": completed}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		offset time.Duration
		want   int64
	}{
		{92*time.Hour - time.Nanosecond, 0}, {92 * time.Hour, 1}, {93 * time.Hour, 0},
	} {
		affected, err := svc.ConfirmDueLedgerEntries(completed.Add(tc.offset))
		if err != nil || affected != tc.want {
			t.Fatalf("offset %s: count %d, err %v", tc.offset, affected, err)
		}
	}
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != resellerdomain.LedgerStatusAvailable || row.ConfirmationMode != resellerdomain.ConfirmationModeAuto || row.ConfirmedBy != nil || row.ConfirmedAt == nil || !row.ConfirmedAt.Equal(completed.Add(92*time.Hour)) || row.ConfirmationReason == "" {
		t.Fatalf("missing automatic audit: %+v", row)
	}
	var balance resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance.AvailableAmountCache.String() != "30.00" {
		t.Fatalf("duplicate/missing balance: %+v", balance)
	}
}

func TestAutomaticSettlementBlocksOrderRefundErrorAndFrozenAccounts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *gorm.DB, orderdomain.Order, resellerdomain.LedgerEntry)
	}{
		{"refund_amount", func(t *testing.T, db *gorm.DB, order orderdomain.Order, row resellerdomain.LedgerEntry) {
			if err := db.Model(&order).Update("refunded_amount", 1).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{"refund_record_without_status_change", func(t *testing.T, db *gorm.DB, order orderdomain.Order, row resellerdomain.LedgerEntry) {
			if err := db.Create(&orderdomain.OrderRefundRecord{OrderID: order.ID, Type: constants.OrderRefundTypeManual, Amount: money.FromDecimal(decimal.NewFromInt(1)), Currency: order.Currency}).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{"refund_debit_without_order_change", func(t *testing.T, db *gorm.DB, order orderdomain.Order, row resellerdomain.LedgerEntry) {
			if err := db.Create(&resellerdomain.LedgerEntry{ResellerID: row.ResellerID, OrderID: &order.ID, Type: resellerdomain.LedgerTypeRefundDeduct, Amount: money.FromDecimal(decimal.NewFromInt(-1)), Currency: row.Currency, Status: resellerdomain.LedgerStatusPendingConfirm, IdempotencyKey: "orphan-refund"}).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{"order_error", func(t *testing.T, db *gorm.DB, order orderdomain.Order, row resellerdomain.LedgerEntry) {
			if err := db.Model(&order).Update("status", "failed").Error; err != nil {
				t.Fatal(err)
			}
		}},
		{"revoked_fulfillment", func(t *testing.T, db *gorm.DB, order orderdomain.Order, row resellerdomain.LedgerEntry) {
			if err := db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", order.ID).Update("status", "failed").Error; err != nil {
				t.Fatal(err)
			}
		}},
		{"profile_frozen", func(t *testing.T, db *gorm.DB, order orderdomain.Order, row resellerdomain.LedgerEntry) {
			if err := db.Model(&resellerdomain.Profile{}).Where("id = ?", row.ResellerID).Update("settlement_status", resellerdomain.SettlementStatusFrozen).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{"profile_suspended", func(t *testing.T, db *gorm.DB, order orderdomain.Order, row resellerdomain.LedgerEntry) {
			if err := db.Model(&resellerdomain.Profile{}).Where("id = ?", row.ResellerID).Update("status", "suspended").Error; err != nil {
				t.Fatal(err)
			}
		}},
		{"balance_frozen", func(t *testing.T, db *gorm.DB, order orderdomain.Order, row resellerdomain.LedgerEntry) {
			if err := db.Model(&resellerdomain.BalanceAccount{}).Where("reseller_id = ?", row.ResellerID).Update("status", resellerdomain.BalanceStatusFrozenReview).Error; err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, _, svc, order, row, completed := seedAutomaticProfit(t)
			tc.mutate(t, db, order, row)
			affected, err := svc.ConfirmDueLedgerEntries(completed.Add(200 * time.Hour))
			if err != nil || affected != 0 {
				t.Fatalf("blocked order released %d: %v", affected, err)
			}
			if err := db.First(&row, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			if row.Status != resellerdomain.LedgerStatusPendingConfirm || row.ConfirmedAt != nil {
				t.Fatalf("blocked row changed money: %+v", row)
			}
		})
	}
}

func TestAutomaticSettlementRequiresLastChildAndNeverUsesPaymentDeadline(t *testing.T) {
	db, _, svc, order, row, completed := seedAutomaticProfit(t)
	if err := db.Where("order_id = ?", order.ID).Delete(&fulfillmentdomain.Fulfillment{}).Error; err != nil {
		t.Fatal(err)
	}
	child1 := orderdomain.Order{ParentID: &order.ID, OrderNo: "auto-child1", Status: constants.OrderStatusCompleted, Currency: order.Currency}
	child2 := orderdomain.Order{ParentID: &order.ID, OrderNo: "auto-child2", Status: constants.OrderStatusFulfilling, Currency: order.Currency}
	if err := db.Create(&child1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&child2).Error; err != nil {
		t.Fatal(err)
	}
	seedDelivery(t, db, child1.ID, completed)
	old := completed.Add(-1000 * time.Hour)
	if err := db.Model(&row).Updates(map[string]interface{}{"confirmation_mode": "auto", "available_at": old}).Error; err != nil {
		t.Fatal(err)
	}
	if affected, err := svc.ConfirmDueLedgerEntries(completed.Add(200 * time.Hour)); err != nil || affected != 0 {
		t.Fatalf("partial delivery count %d: %v", affected, err)
	}
	if err := db.Model(&child2).Update("status", constants.OrderStatusCompleted).Error; err != nil {
		t.Fatal(err)
	}
	seedDelivery(t, db, child2.ID, completed.Add(10*time.Hour))
	if affected, err := svc.ConfirmDueLedgerEntries(completed.Add(101 * time.Hour)); err != nil || affected != 0 {
		t.Fatalf("last child held too briefly %d: %v", affected, err)
	}
	if affected, err := svc.ConfirmDueLedgerEntries(completed.Add(102 * time.Hour)); err != nil || affected != 1 {
		t.Fatalf("last child not released %d: %v", affected, err)
	}
}

func TestAutomaticSettlementUnfreezeReevaluatesWithoutDoubleCredit(t *testing.T) {
	db, _, svc, _, row, completed := seedAutomaticProfit(t)
	if err := db.Model(&resellerdomain.Profile{}).Where("id = ?", row.ResellerID).Update("settlement_status", resellerdomain.SettlementStatusFrozen).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := svc.ConfirmDueLedgerEntries(completed.Add(100 * time.Hour)); err != nil || count != 0 {
		t.Fatalf("frozen %d %v", count, err)
	}
	if err := db.Model(&resellerdomain.Profile{}).Where("id = ?", row.ResellerID).Update("settlement_status", resellerdomain.SettlementStatusNormal).Error; err != nil {
		t.Fatal(err)
	}
	if count, err := svc.ConfirmDueLedgerEntries(completed.Add(100 * time.Hour)); err != nil || count != 1 {
		t.Fatalf("unfrozen %d %v", count, err)
	}
	if count, err := svc.ConfirmDueLedgerEntries(completed.Add(101 * time.Hour)); err != nil || count != 0 {
		t.Fatalf("replayed %d %v", count, err)
	}
}

func TestAutomaticSettlementRepairsMatchingDeadlineWithMissingDeliveryAnchor(t *testing.T) {
	db, _, svc, _, row, completed := seedAutomaticProfit(t)
	deadline := completed.Add(92 * time.Hour)
	if err := db.Model(&row).Updates(map[string]interface{}{"available_at": deadline, "delivery_completed_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ConfirmDueLedgerEntries(deadline); err != nil || n != 1 {
		t.Fatalf("matching legacy deadline stranded profit: %d %v", n, err)
	}
}

func TestAutomaticSettlementConcurrentJobsReleaseOnlyOnce(t *testing.T) {
	db, _, svc, _, row, completed := seedAutomaticProfit(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	start := make(chan struct{})
	counts := make(chan int64, 8)
	errs := make(chan error, 8)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			n, e := svc.ConfirmDueLedgerEntries(completed.Add(92 * time.Hour))
			counts <- n
			errs <- e
		}()
	}
	close(start)
	group.Wait()
	close(counts)
	close(errs)
	var total int64
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for n := range counts {
		total += n
	}
	if total != 1 {
		t.Fatalf("concurrent workers released %d times", total)
	}
	var balance resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance.AvailableAmountCache.String() != "30.00" {
		t.Fatalf("incorrect concurrent balance: %+v", balance)
	}
}

func TestAutomaticSettlementConcurrentRefundNeverLeavesSpendableProfit(t *testing.T) {
	db, repo, svc, order, row, completed := seedAutomaticProfit(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() { <-start; _, e := svc.ConfirmDueLedgerEntries(completed.Add(92 * time.Hour)); errs <- e }()
	go func() {
		<-start
		errs <- db.Transaction(func(tx *gorm.DB) error {
			bound := repo.BindTx(tx)
			locked, e := bound.GetSettlementOrderForUpdate(order.ID)
			if e != nil {
				return e
			}
			record := orderdomain.OrderRefundRecord{OrderID: order.ID, UserID: order.UserID, Type: constants.OrderRefundTypeManual, Amount: order.TotalAmount, Currency: order.Currency}
			if e = tx.Create(&record).Error; e != nil {
				return e
			}
			if e = svc.HandleRefundDeduct(bound, locked, &record, decimal.Zero); e != nil {
				return e
			}
			return tx.Model(&order).Updates(map[string]interface{}{"refunded_amount": order.TotalAmount, "status": constants.OrderStatusRefunded}).Error
		})
	}()
	close(start)
	for i := 0; i < 2; i++ {
		if e := <-errs; e != nil {
			t.Fatal(e)
		}
	}
	if _, e := svc.ConfirmDueLedgerEntries(completed.Add(100 * time.Hour)); e != nil {
		t.Fatal(e)
	}
	var balance resellerdomain.BalanceAccount
	if e := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; e != nil {
		t.Fatal(e)
	}
	if balance.AvailableAmountCache.String() != "0.00" || balance.NegativeAmountCache.String() != "0.00" {
		t.Fatalf("refunded profit became spendable: %+v", balance)
	}
}

func TestAutomaticSettlementRefundAfterReleaseClawsBackProfit(t *testing.T) {
	db, repo, svc, order, row, completed := seedAutomaticProfit(t)
	if n, err := svc.ConfirmDueLedgerEntries(completed.Add(92 * time.Hour)); err != nil || n != 1 {
		t.Fatalf("release %d %v", n, err)
	}
	refund := orderdomain.OrderRefundRecord{OrderID: order.ID, UserID: order.UserID, Type: constants.OrderRefundTypeManual, Amount: order.TotalAmount, Currency: order.Currency}
	if err := db.Create(&refund).Error; err != nil {
		t.Fatal(err)
	}
	post := func() error {
		return db.Transaction(func(tx *gorm.DB) error { return svc.HandleRefundDeduct(repo.BindTx(tx), &order, &refund, decimal.Zero) })
	}
	if err := post(); err != nil {
		t.Fatal(err)
	}
	if err := post(); err != nil {
		t.Fatal(err)
	}
	var balance resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance.AvailableAmountCache.String() != "0.00" {
		t.Fatalf("refund failed to deduct: %+v", balance)
	}
	var debit resellerdomain.LedgerEntry
	if err := db.Where("type = ?", resellerdomain.LedgerTypeRefundDeduct).First(&debit).Error; err != nil {
		t.Fatal(err)
	}
	if debit.Status != resellerdomain.LedgerStatusAvailable || debit.Amount.String() != "-30.00" {
		t.Fatalf("refund mismatch: %+v", debit)
	}
}
