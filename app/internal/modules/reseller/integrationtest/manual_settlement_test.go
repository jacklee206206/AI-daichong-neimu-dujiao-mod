// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	resellerapp "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	resellergormstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func seedManualProfit(t *testing.T) (*gorm.DB, *resellergormstore.Store, *resellerapp.AccountingLedgerService, orderdomain.Order, resellerdomain.LedgerEntry) {
	t.Helper()
	db := openResellerAccountingServiceTestDB(t)
	order, payment, snapshot := seedPaidResellerOrderSnapshot(t, db, true)
	// Exercise the actual self-purchase policy throughout the manual-settlement lifecycle.
	ctx := &resellercontract.OrderPricingContext{
		ResellerUserID: snapshot.ResellerUserID,
		BuyerUserID:    snapshot.BuyerUserID,
		ProfitEligible: true,
	}
	if ctx.BuyerUserID != ctx.ResellerUserID {
		t.Fatal("manual settlement fixture must represent an owner purchase")
	}
	resellercontract.ApplySelfDealingRisk(ctx, &resellerdomain.Profile{UserID: snapshot.ResellerUserID}, false)
	snapshot.ProfitEligible = ctx.ProfitEligible
	snapshot.ProfitBlockReason = ctx.ProfitBlockReason
	snapshot.RiskSnapshotJSON = ctx.RiskSnapshot
	if err := db.Save(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	repo := resellergormstore.New(db)
	svc := resellerapp.NewAccountingLedgerService(repo, 0, resellerdomain.ConfirmationModeManual) // Explicit emergency manual review policy.
	if err := db.Transaction(func(tx *gorm.DB) error { return svc.PostOrderProfit(repo.BindTx(tx), &order, &payment) }); err != nil {
		t.Fatal(err)
	}
	var row resellerdomain.LedgerEntry
	if err := db.Where("order_id = ?", order.ID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return db, repo, svc, order, row
}

func seedDelivery(t *testing.T, db *gorm.DB, id uint, at time.Time) {
	t.Helper()
	row := fulfillmentdomain.Fulfillment{OrderID: id, Type: constants.FulfillmentTypeAuto, Status: constants.FulfillmentStatusDelivered, Payload: "test-only", DeliveredAt: &at, CreatedAt: at, UpdatedAt: at}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func TestOwnerPurchaseProfitIsPendingAndIdempotent(t *testing.T) {
	db, repo, svc, order, row := seedManualProfit(t)
	if row.Status != resellerdomain.LedgerStatusPendingConfirm || row.Amount.String() != "30.00" || row.AvailableAt != nil {
		t.Fatalf("owner margin must enter pending balance until delivery and manual approval: %+v", row)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return svc.PostOrderProfitForOrder(repo.BindTx(tx), &order)
	}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&resellerdomain.LedgerEntry{}).Where("order_id = ? AND type = ?", order.ID, resellerdomain.LedgerTypeOrderProfit).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("repeat payment notification duplicated owner profit: %d", count)
	}
	var balance resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ? AND currency = ?", row.ResellerID, row.Currency).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance.AvailableAmountCache.String() != "0.00" {
		t.Fatalf("owner margin credited to incorrect balance: %+v", balance)
	}
}

func TestManualSettlementPaymentAndSpoofedCompletionDoNotStartHold(t *testing.T) {
	db, _, svc, order, row := seedManualProfit(t)
	if row.AvailableAt != nil || row.ConfirmationMode != resellerdomain.ConfirmationModeManual {
		t.Fatalf("payment must not start hold: %+v", row)
	}
	if err := db.Model(&order).Update("status", constants.OrderStatusCompleted).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmDueLedgerEntries(time.Now().Add(1000 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "checked"); !errors.Is(err, resellercontract.ErrLedgerNotReady) {
		t.Fatalf("missing delivery must fail: %v", err)
	}
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.AvailableAt != nil || row.Status != resellerdomain.LedgerStatusPendingConfirm {
		t.Fatalf("no real fulfillment: %+v", row)
	}
}

func TestManualSettlementAllChildrenMustDeliverAndBackdateCannotShortenHold(t *testing.T) {
	db, _, svc, order, row := seedManualProfit(t)
	child1 := orderdomain.Order{ParentID: &order.ID, OrderNo: "child1", Status: constants.OrderStatusCompleted, Currency: "USD"}
	child2 := orderdomain.Order{ParentID: &order.ID, OrderNo: "child2", Status: constants.OrderStatusFulfilling, Currency: "USD"}
	if err := db.Create(&child1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&child2).Error; err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-120 * time.Hour)
	seedDelivery(t, db, child1.ID, old)
	if err := svc.RefreshPendingDeliveryEligibility(); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.AvailableAt != nil {
		t.Fatal("partial delivery started hold")
	}
	seedDelivery(t, db, child2.ID, old)
	now := time.Now()
	if err := db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", child2.ID).Update("created_at", now).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshPendingDeliveryEligibility(); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.AvailableAt == nil || row.DeliveryCompletedAt == nil || row.AvailableAt.Sub(*row.DeliveryCompletedAt) != 92*time.Hour || row.DeliveryCompletedAt.Before(now.Add(-time.Millisecond)) {
		t.Fatalf("hold must be exactly92h from latest actual record: %+v", row)
	}
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "checked"); !errors.Is(err, resellercontract.ErrLedgerNotReady) {
		t.Fatalf("backdate must not release: %v", err)
	}
}

func TestManualSettlementMaturityStillRequiresApprovalAndIsIdempotent(t *testing.T) {
	db, repo, svc, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-97*time.Hour))
	affected, err := svc.ConfirmDueLedgerEntries(time.Now())
	if err != nil || affected != 0 {
		t.Fatalf("manual worker must not release: %d %v", affected, err)
	}
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != resellerdomain.LedgerStatusPendingConfirm {
		t.Fatal("matured without approval")
	}
	if _, err := svc.ConfirmOrderProfit(99, row.ID, ""); !errors.Is(err, resellercontract.ErrLedgerReviewReasonRequired) {
		t.Fatal(err)
	}
	approved, err := svc.ConfirmOrderProfit(99, row.ID, "真实交付与退款已核对")
	if err != nil {
		t.Fatal(err)
	}
	if approved.ConfirmedBy == nil || *approved.ConfirmedBy != 99 || approved.ConfirmedAt == nil || approved.Status != resellerdomain.LedgerStatusAvailable {
		t.Fatalf("missing audit: %+v", approved)
	}
	if _, err := svc.ConfirmOrderProfit(100, row.ID, "duplicate"); err != nil {
		t.Fatal(err)
	}
	loaded, _ := repo.GetLedgerEntryByID(row.ID)
	if *loaded.ConfirmedBy != 99 || loaded.ConfirmationReason != "真实交付与退款已核对" {
		t.Fatal("repeat overwrote audit")
	}
	var balance resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance.AvailableAmountCache.String() != "30.00" {
		t.Fatal(balance.AvailableAmountCache)
	}
}

func TestManualSettlementConfirmsRefundDebitsTogether(t *testing.T) {
	db, repo, svc, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-97*time.Hour))
	refund := orderdomain.OrderRefundRecord{OrderID: order.ID, UserID: order.UserID, Type: constants.OrderRefundTypeManual, Amount: money.FromDecimal(decimal.NewFromInt(65)), Currency: "USD"}
	if err := db.Create(&refund).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return svc.HandleRefundDeduct(repo.BindTx(tx), &order, &refund, decimal.Zero) }); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "net checked"); err != nil {
		t.Fatal(err)
	}
	var rows []resellerdomain.LedgerEntry
	if err := db.Where("order_id = ?", order.ID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatal(len(rows))
	}
	for _, item := range rows {
		if item.Status != resellerdomain.LedgerStatusAvailable || item.ConfirmedAt == nil {
			t.Fatalf("debit not atomically approved: %+v", item)
		}
	}
	var balance resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance.AvailableAmountCache.String() != "15.00" {
		t.Fatal(balance.AvailableAmountCache)
	}
}

func TestManualSettlementHoldsBefore92Hours(t *testing.T) {
	db, _, svc, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-91*time.Hour))
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "checked"); !errors.Is(err, resellercontract.ErrLedgerNotReady) {
		t.Fatalf("early review: %v", err)
	}
}

func TestWithdrawPayRechecksRefundAfterApplication(t *testing.T) {
	db, repo, svc, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-97*time.Hour))
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "checked"); err != nil {
		t.Fatal(err)
	}
	withdraw := resellerapp.NewAccountingWithdrawService(repo)
	request, err := withdraw.ApplyWithdraw(row.ResellerID, resellercontract.WithdrawApplyInput{Amount: decimal.NewFromInt(30), Currency: "USD", Channel: "manual", Account: "test"})
	if err != nil {
		t.Fatal(err)
	}
	refund := orderdomain.OrderRefundRecord{OrderID: order.ID, UserID: order.UserID, Type: constants.OrderRefundTypeManual, Amount: money.FromDecimal(decimal.NewFromInt(130)), Currency: "USD"}
	if err := db.Create(&refund).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return svc.HandleRefundDeduct(repo.BindTx(tx), &order, &refund, decimal.Zero) }); err != nil {
		t.Fatal(err)
	}
	if _, err := withdraw.ReviewWithdraw(99, request.ID, resellercontract.WithdrawActionPay, ""); !errors.Is(err, resellercontract.ErrBalanceAccountFrozen) {
		t.Fatalf("refunded locked funds must not be paid: %v", err)
	}
	current, err := repo.GetWithdrawRequestByID(request.ID)
	if err != nil || current.Status != resellerdomain.WithdrawStatusPending {
		t.Fatalf("request changed despite rejection: %+v %v", current, err)
	}
	if _, err := withdraw.ReviewWithdraw(99, request.ID, resellercontract.WithdrawActionReject, "refund"); err != nil {
		t.Fatal(err)
	}
	var b resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&b).Error; err != nil {
		t.Fatal(err)
	}
	if b.AvailableAmountCache.String() != "0.00" || b.LockedAmountCache.String() != "0.00" {
		t.Fatal(fmt.Sprintf("unexpected balance %+v", b))
	}
}

func TestManualSettlementPendingProfitCannotBeWithdrawn(t *testing.T) {
	db, repo, _, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-120*time.Hour))
	_, err := resellerapp.NewAccountingWithdrawService(repo).ApplyWithdraw(row.ResellerID, resellercontract.WithdrawApplyInput{Amount: decimal.NewFromInt(1), Currency: row.Currency, Channel: "manual", Account: "test"})
	if !errors.Is(err, resellercontract.ErrWithdrawInsufficient) {
		t.Fatalf("matured but unconfirmed credit was spendable: %v", err)
	}
}

func TestManualSettlementRevokedDeliveryInvalidatesPreviouslyReadyEntry(t *testing.T) {
	db, _, svc, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-120*time.Hour))
	if err := svc.RefreshPendingDeliveryEligibility(); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&fulfillmentdomain.Fulfillment{}).Where("order_id = ?", order.ID).Update("status", "pending").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "checked"); !errors.Is(err, resellercontract.ErrLedgerNotReady) {
		t.Fatalf("revoked delivery was approved: %v", err)
	}
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != resellerdomain.LedgerStatusPendingConfirm || row.ConfirmedAt != nil {
		t.Fatal("rejected approval altered ledger")
	}
}

func TestManualSettlementFullRefundCancelsCreditAndDebitAtApproval(t *testing.T) {
	db, repo, svc, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-120*time.Hour))
	refund := orderdomain.OrderRefundRecord{OrderID: order.ID, UserID: order.UserID, Type: constants.OrderRefundTypeManual, Amount: money.FromDecimal(decimal.NewFromInt(130)), Currency: row.Currency}
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
	approved, err := svc.ConfirmOrderProfit(99, row.ID, "full refund checked")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != resellerdomain.LedgerStatusCanceled {
		t.Fatalf("fully refunded credit remained available: %+v", approved)
	}
	var rows []resellerdomain.LedgerEntry
	if err := db.Where("order_id = ?", order.ID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("duplicate refund posted %d rows", len(rows))
	}
	for _, r := range rows {
		if r.Status != resellerdomain.LedgerStatusCanceled || r.ConfirmedAt == nil {
			t.Fatalf("debit not canceled/audited: %+v", r)
		}
	}
	var balance resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if !balance.AvailableAmountCache.Decimal.IsZero() || !balance.LockedAmountCache.Decimal.IsZero() {
		t.Fatalf("refunded money released: %+v", balance)
	}
}

func TestManualSettlementReadinessUsesServerFulfillmentWithoutWorker(t *testing.T) {
	db, repo, _, order, row := seedManualProfit(t)
	query := resellerapp.NewAccountingQueryService(repo)
	read := func() resellerdomain.LedgerEntry {
		t.Helper()
		rows, _, err := query.ListAdminLedgerEntries(resellercontract.AdminLedgerListFilter{OrderID: order.ID})
		if err != nil || len(rows) != 1 {
			t.Fatalf("list: %v %+v", err, rows)
		}
		return rows[0]
	}
	if got := read(); got.ReviewState != "awaiting_delivery" || got.CanConfirm {
		t.Fatalf("missing fulfillment: %+v", got)
	}
	seedDelivery(t, db, order.ID, time.Now().Add(-120*time.Hour))
	if got := read(); got.ReviewState != "blocked" || got.CanConfirm || got.AvailableAt == nil {
		t.Fatalf("mature fulfillment not ready: %+v", got)
	}
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.AvailableAt != nil || row.ConfirmedAt != nil || row.Status != resellerdomain.LedgerStatusPendingConfirm {
		t.Fatal("read-only list mutated money or stored deadline")
	}
	if err := db.Model(&resellerdomain.BalanceAccount{}).Where("reseller_id = ?", row.ResellerID).Update("status", resellerdomain.BalanceStatusFrozenReview).Error; err != nil {
		t.Fatal(err)
	}
	if got := read(); got.ReviewState != "blocked" || got.CanConfirm {
		t.Fatalf("frozen balance appears approvable: %+v", got)
	}
}

func TestWithdrawCannotBypassSuspendedProfileThroughDirectUseCase(t *testing.T) {
	db, repo, svc, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-120*time.Hour))
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "checked"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&resellerdomain.Profile{}).Where("id = ?", row.ResellerID).Update("status", "suspended").Error; err != nil {
		t.Fatal(err)
	}
	_, err := resellerapp.NewAccountingWithdrawService(repo).ApplyWithdraw(row.ResellerID, resellercontract.WithdrawApplyInput{Amount: decimal.NewFromInt(1), Currency: row.Currency, Channel: "manual", Account: "test"})
	if !errors.Is(err, resellercontract.ErrProfileInactive) {
		t.Fatalf("inactive profile bypassed direct withdraw: %v", err)
	}
}

func TestRefundAndBalanceRefreshCannotEraseAdministrativeFreeze(t *testing.T) {
	db, repo, svc, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-120*time.Hour))
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "checked"); err != nil {
		t.Fatal(err)
	}
	request, err := resellerapp.NewAccountingWithdrawService(repo).ApplyWithdraw(row.ResellerID, resellercontract.WithdrawApplyInput{Amount: decimal.NewFromInt(30), Currency: row.Currency, Channel: "manual", Account: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&resellerdomain.BalanceAccount{}).Where("reseller_id = ?", row.ResellerID).Update("status", resellerdomain.BalanceStatusFrozenReview).Error; err != nil {
		t.Fatal(err)
	}
	refund := orderdomain.OrderRefundRecord{OrderID: order.ID, UserID: order.UserID, Type: constants.OrderRefundTypeManual, Amount: money.FromDecimal(decimal.NewFromInt(130)), Currency: row.Currency}
	if err := db.Create(&refund).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return svc.HandleRefundDeduct(repo.BindTx(tx), &order, &refund, decimal.Zero) }); err != nil {
		t.Fatal(err)
	}
	if _, err := resellerapp.NewAccountingWithdrawService(repo).ReviewWithdraw(99, request.ID, resellercontract.WithdrawActionReject, "refund"); err != nil {
		t.Fatal(err)
	}
	var balance resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance.Status != resellerdomain.BalanceStatusFrozenReview {
		t.Fatalf("refund or rejection erased admin freeze: %+v", balance)
	}
	if !balance.AvailableAmountCache.Decimal.IsZero() || !balance.LockedAmountCache.Decimal.IsZero() {
		t.Fatalf("refund ledger incorrect: %+v", balance)
	}
}

func TestConcurrentWithdrawCannotReserveSameConfirmedProfitTwice(t *testing.T) {
	db, repo, svc, order, row := seedManualProfit(t)
	seedDelivery(t, db, order.ID, time.Now().Add(-120*time.Hour))
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "checked"); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := resellerapp.NewAccountingWithdrawService(repo).ApplyWithdraw(row.ResellerID, resellercontract.WithdrawApplyInput{Amount: decimal.NewFromInt(30), Currency: row.Currency, Channel: "manual", Account: "test"})
			results <- err
		}()
	}
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, resellercontract.ErrWithdrawInsufficient) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one reserve, got %d", successes)
	}
	var requests int64
	db.Model(&resellerdomain.WithdrawRequest{}).Count(&requests)
	var balance resellerdomain.BalanceAccount
	db.Where("reseller_id = ?", row.ResellerID).First(&balance)
	if requests != 1 || balance.LockedAmountCache.String() != "30.00" || balance.AvailableAmountCache.String() != "0.00" {
		t.Fatalf("double reserve requests=%d balance=%+v", requests, balance)
	}
}

func TestChildRefundDebitsParentProfitAndRemainingDeliveredGoodsCanSettle(t *testing.T) {
	db, repo, svc, order, row := seedManualProfit(t)
	child1 := orderdomain.Order{ParentID: &order.ID, OrderNo: "refund-child-1", ResellerID: order.ResellerID, Status: constants.OrderStatusPaid, PaidAt: order.PaidAt, Currency: row.Currency, TotalAmount: money.FromDecimal(decimal.NewFromInt(80))}
	child2 := orderdomain.Order{ParentID: &order.ID, OrderNo: "refund-child-2", ResellerID: order.ResellerID, Status: constants.OrderStatusPaid, PaidAt: order.PaidAt, Currency: row.Currency, TotalAmount: money.FromDecimal(decimal.NewFromInt(50))}
	if err := db.Create(&child1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&child2).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot resellerdomain.OrderSnapshot
	if err := db.Where("order_id = ?", order.ID).First(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	snapshot.PricingSnapshotJSON["items"] = []map[string]interface{}{
		{"child_order_id": child1.ID, "order_item_id": 1, "profit_amount": "20.00"},
		{"child_order_id": child2.ID, "order_item_id": 2, "profit_amount": "10.00"},
	}
	if err := db.Save(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	refundChild := func(amount, before int64) {
		t.Helper()
		record := orderdomain.OrderRefundRecord{OrderID: child1.ID, Type: constants.OrderRefundTypeManual, Amount: money.FromDecimal(decimal.NewFromInt(amount)), Currency: row.Currency}
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
		apply := func() error {
			return db.Transaction(func(tx *gorm.DB) error {
				return svc.HandleRefundDeduct(repo.BindTx(tx), &child1, &record, decimal.NewFromInt(before))
			})
		}
		if err := apply(); err != nil {
			t.Fatal(err)
		}
		if err := apply(); err != nil {
			t.Fatal(err)
		}
	}
	refundChild(40, 0)
	refundChild(40, 40)
	var deductions []resellerdomain.LedgerEntry
	if err := db.Where("type = ?", resellerdomain.LedgerTypeRefundDeduct).Find(&deductions).Error; err != nil {
		t.Fatal(err)
	}
	if len(deductions) != 2 {
		t.Fatalf("refund replay duplicated child debits: %+v", deductions)
	}
	total := decimal.Zero
	for _, debit := range deductions {
		if debit.OrderID == nil || *debit.OrderID != order.ID || debit.Status != resellerdomain.LedgerStatusPendingConfirm {
			t.Fatalf("debit must share pending parent account: %+v", debit)
		}
		total = total.Add(debit.Amount.Decimal)
	}
	if !total.Equal(decimal.NewFromInt(-20)) {
		t.Fatalf("child's 20 margin not deducted: %s", total)
	}
	if err := db.Model(&child1).Updates(map[string]interface{}{"status": constants.OrderStatusRefunded, "refunded_amount": child1.TotalAmount}).Error; err != nil {
		t.Fatal(err)
	}
	seedDelivery(t, db, child2.ID, time.Now().Add(-120*time.Hour))
	if _, err := svc.ConfirmOrderProfit(99, row.ID, "child refund and other delivery checked"); err != nil {
		t.Fatal(err)
	}
	var balance resellerdomain.BalanceAccount
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance.AvailableAmountCache.String() != "10.00" {
		t.Fatalf("only second child's margin should remain: %+v", balance)
	}
	// A subsequent parent refund can never deduct more than the remaining 10.
	parentRefund := orderdomain.OrderRefundRecord{OrderID: order.ID, Type: constants.OrderRefundTypeManual, Amount: order.TotalAmount, Currency: row.Currency}
	if err := db.Create(&parentRefund).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return svc.HandleRefundDeduct(repo.BindTx(tx), &order, &parentRefund, decimal.Zero)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("reseller_id = ?", row.ResellerID).First(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance.AvailableAmountCache.String() != "0.00" {
		t.Fatalf("mixed parent/child refund over/under deducted: %+v", balance)
	}
}
