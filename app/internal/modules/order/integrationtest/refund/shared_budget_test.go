// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package refund_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	. "github.com/dujiao-next/internal/modules/order/application/refund"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	walletdomain "github.com/dujiao-next/internal/modules/wallet/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

func executeSharedBudgetRefund(svc *Service, method string, orderID uint, amount int64) error {
	if method == "wallet" {
		_, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{OrderID: orderID, Amount: money.FromDecimal(decimal.NewFromInt(amount))})
		return err
	}
	_, _, err := svc.AdminManualRefund(AdminManualRefundInput{OrderID: orderID, Amount: money.FromDecimal(decimal.NewFromInt(amount))})
	return err
}

// Parent and child orders describe one payment. Different refund methods must
// consume the same payment budget instead of each receiving a fresh allowance.
func TestRefundSharesBudgetAcrossParentChildrenAndMethods(t *testing.T) {
	for _, firstMethod := range []string{"manual", "wallet"} {
		for _, secondMethod := range []string{"manual", "wallet"} {
			for _, parentFirst := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s_then_%s_parentFirst_%t", firstMethod, secondMethod, parentFirst), func(t *testing.T) {
					svc, db := setupOrderRefundWalletTest(t)
					createTestUser(t, db, 301)
					parent := createTestOrder(t, db, 301, "SHARED-REFUND", decimal.NewFromInt(125))
					now := time.Now()
					parent.PaidAt = &now
					parent.Status = constants.OrderStatusCompleted
					if err := db.Save(parent).Error; err != nil {
						t.Fatal(err)
					}
					child := createTestChildOrderWithFulfillmentType(t, db, parent, "SHARED-REFUND-01", constants.OrderStatusCompleted, decimal.NewFromInt(125), constants.FulfillmentTypeAuto, true)
					refund := func(method string, orderID uint, amount int64) error {
						if method == "wallet" {
							_, _, _, err := svc.AdminRefundToWallet(AdminRefundToWalletInput{OrderID: orderID, Amount: money.FromDecimal(decimal.NewFromInt(amount))})
							return err
						}
						_, _, err := svc.AdminManualRefund(AdminManualRefundInput{OrderID: orderID, Amount: money.FromDecimal(decimal.NewFromInt(amount))})
						return err
					}
					first, second := parent.ID, child.ID
					if !parentFirst {
						first, second = child.ID, parent.ID
					}
					if err := refund(firstMethod, first, 125); err != nil {
						t.Fatal(err)
					}
					if err := refund(secondMethod, second, 1); !errors.Is(err, walletcontract.ErrRefundExceeded) {
						t.Fatalf("expected shared payment budget to reject second refund, got %v", err)
					}
					var records []orderdomain.OrderRefundRecord
					if err := db.Find(&records).Error; err != nil {
						t.Fatal(err)
					}
					if len(records) != 1 {
						t.Fatalf("unexpected extra refund record: %d", len(records))
					}
				})
			}
		}
	}
}

func TestRefundRejectsChangingScopeWithoutSideEffects(t *testing.T) {
	for _, guest := range []bool{false, true} {
		for _, parentFirst := range []bool{false, true} {
			for _, secondMethod := range []string{"manual", "wallet"} {
				if guest && secondMethod == "wallet" {
					continue
				}
				t.Run(fmt.Sprintf("guest_%t_parentFirst_%t_second_%s", guest, parentFirst, secondMethod), func(t *testing.T) {
					svc, db := setupOrderRefundWalletTest(t)
					userID := uint(0)
					if !guest {
						userID = 302
						createTestUser(t, db, userID)
					}
					parent := createTestOrder(t, db, userID, "REFUND-SCOPE", decimal.NewFromInt(125))
					now := time.Now()
					parent.PaidAt = &now
					parent.Status = constants.OrderStatusCompleted
					if guest {
						parent.GuestEmail = "scope@example.com"
					}
					if err := db.Save(parent).Error; err != nil {
						t.Fatal(err)
					}
					child := createTestChildOrderWithFulfillmentType(t, db, parent, "REFUND-SCOPE-01", constants.OrderStatusCompleted, decimal.NewFromInt(125), constants.FulfillmentTypeAuto, true)
					first, second := parent.ID, child.ID
					if !parentFirst {
						first, second = child.ID, parent.ID
					}
					if err := executeSharedBudgetRefund(svc, "manual", first, 20); err != nil {
						t.Fatal(err)
					}
					if err := executeSharedBudgetRefund(svc, secondMethod, second, 5); !errors.Is(err, ErrOrderRefundScopeConflict) {
						t.Fatalf("expected scope conflict, got %v", err)
					}
					var count int64
					if err := db.Model(&orderdomain.OrderRefundRecord{}).Count(&count).Error; err != nil || count != 1 {
						t.Fatalf("refund rows=%d err=%v", count, err)
					}
					if err := db.Model(&walletdomain.Transaction{}).Count(&count).Error; err != nil || count != 0 {
						t.Fatalf("wallet rows=%d err=%v", count, err)
					}
					var selected orderdomain.Order
					if err := db.First(&selected, second).Error; err != nil {
						t.Fatal(err)
					}
					if !selected.RefundedAmount.Decimal.IsZero() {
						t.Fatal("rejected refund changed order amount")
					}
				})
			}
		}
	}
}

func TestRefundMultipleSKUsMayRefundSeparatelyToFullTotal(t *testing.T) {
	for _, guest := range []bool{false, true} {
		t.Run(fmt.Sprintf("guest_%t", guest), func(t *testing.T) {
			svc, db := setupOrderRefundWalletTest(t)
			userID := uint(0)
			if !guest {
				userID = 303
				createTestUser(t, db, userID)
			}
			parent := createTestOrder(t, db, userID, "REFUND-MULTI", decimal.NewFromInt(100))
			now := time.Now()
			parent.PaidAt = &now
			parent.Status = constants.OrderStatusCompleted
			if guest {
				parent.GuestEmail = "multi@example.com"
			}
			if err := db.Save(parent).Error; err != nil {
				t.Fatal(err)
			}
			first := createTestChildOrderWithFulfillmentType(t, db, parent, "REFUND-MULTI-01", constants.OrderStatusCompleted, decimal.NewFromInt(60), constants.FulfillmentTypeAuto, true)
			second := createTestChildOrderWithFulfillmentType(t, db, parent, "REFUND-MULTI-02", constants.OrderStatusCompleted, decimal.NewFromInt(40), constants.FulfillmentTypeAuto, true)
			if err := executeSharedBudgetRefund(svc, "manual", first.ID, 20); err != nil {
				t.Fatal(err)
			}
			method := "wallet"
			if guest {
				method = "manual"
			}
			if err := executeSharedBudgetRefund(svc, method, first.ID, 41); !errors.Is(err, walletcontract.ErrRefundExceeded) {
				t.Fatalf("expected item budget exceeded, got %v", err)
			}
			if err := executeSharedBudgetRefund(svc, method, first.ID, 40); err != nil {
				t.Fatal(err)
			}
			if err := executeSharedBudgetRefund(svc, "manual", parent.ID, 1); !errors.Is(err, ErrOrderRefundScopeConflict) {
				t.Fatalf("expected original scope enforced, got %v", err)
			}
			if err := executeSharedBudgetRefund(svc, method, second.ID, 40); err != nil {
				t.Fatal(err)
			}
			var records []orderdomain.OrderRefundRecord
			if err := db.Find(&records).Error; err != nil {
				t.Fatal(err)
			}
			total := decimal.Zero
			for _, record := range records {
				total = total.Add(record.Amount.Decimal)
			}
			if len(records) != 3 || !total.Equal(decimal.NewFromInt(100)) {
				t.Fatalf("records=%d refund total=%s", len(records), total)
			}
			var reloaded orderdomain.Order
			if err := db.First(&reloaded, parent.ID).Error; err != nil {
				t.Fatal(err)
			}
			if reloaded.Status != constants.OrderStatusRefunded {
				t.Fatalf("expected fully refunded parent, got %s", reloaded.Status)
			}
			if err := executeSharedBudgetRefund(svc, method, first.ID, 1); !errors.Is(err, walletcontract.ErrRefundExceeded) {
				t.Fatalf("expected exhausted shared budget, got %v", err)
			}
		})
	}
}

func TestRefundUsesRecordsWhenAmountCacheIsStale(t *testing.T) {
	svc, db := setupOrderRefundWalletTest(t)
	createTestUser(t, db, 304)
	order := createTestOrder(t, db, 304, "REFUND-STALE", decimal.NewFromInt(100))
	now := time.Now()
	order.PaidAt = &now
	order.Status = constants.OrderStatusCompleted
	if err := db.Save(order).Error; err != nil {
		t.Fatal(err)
	}
	if err := executeSharedBudgetRefund(svc, "manual", order.ID, 60); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(order).Update("refunded_amount", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := executeSharedBudgetRefund(svc, "wallet", order.ID, 41); !errors.Is(err, walletcontract.ErrRefundExceeded) {
		t.Fatalf("expected record-derived budget, got %v", err)
	}
	if err := executeSharedBudgetRefund(svc, "wallet", order.ID, 40); err != nil {
		t.Fatal(err)
	}
	if err := db.First(order, order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !order.RefundedAmount.Decimal.Equal(decimal.NewFromInt(100)) || order.Status != constants.OrderStatusRefunded {
		t.Fatalf("stale cache not recovered: refunded=%s status=%s", order.RefundedAmount.String(), order.Status)
	}
}
