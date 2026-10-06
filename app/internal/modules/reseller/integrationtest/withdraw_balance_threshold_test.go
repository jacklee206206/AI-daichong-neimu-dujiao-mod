// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package integrationtest

import (
	"errors"
	"testing"

	resellerapp "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	resellergormstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

func TestWithdrawRequiresNetAvailableCNY50(t *testing.T) {
	for _, tc := range []struct {
		name, credit, refund, request string
		frozen                        bool
		want                          error
	}{
		{"below boundary", "49.99", "0", "1", false, resellercontract.ErrWithdrawBalanceBelowMinimum},
		{"exact boundary", "50", "0", "50", false, nil},
		{"threshold is balance not request", "50", "0", "1", false, nil},
		{"refund reduces eligibility", "100", "-50.01", "1", false, resellercontract.ErrWithdrawBalanceBelowMinimum},
		{"exact net boundary", "100", "-50", "50", false, nil},
		{"cannot overdraw", "50", "0", "50.01", false, resellercontract.ErrWithdrawInsufficient},
		{"frozen even above threshold", "100", "0", "50", true, resellercontract.ErrBalanceAccountFrozen},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openResellerAccountingServiceTestDB(t)
			profile := seedResellerAccountingProfile(t, db)
			rows := []resellerdomain.LedgerEntry{
				{ResellerID: profile.ID, Type: resellerdomain.LedgerTypeOrderProfit, Currency: "CNY", Amount: money.FromDecimal(decimal.RequireFromString(tc.credit)), Status: resellerdomain.LedgerStatusAvailable, IdempotencyKey: "credit"},
				{ResellerID: profile.ID, Type: resellerdomain.LedgerTypeRefundDeduct, Currency: "CNY", Amount: money.FromDecimal(decimal.RequireFromString(tc.refund)), Status: resellerdomain.LedgerStatusAvailable, IdempotencyKey: "refund"},
				// None of these may inflate the net available CNY balance.
				{ResellerID: profile.ID, Type: resellerdomain.LedgerTypeOrderProfit, Currency: "CNY", Amount: money.FromDecimal(decimal.NewFromInt(100)), Status: resellerdomain.LedgerStatusPendingConfirm, IdempotencyKey: "pending"},
				{ResellerID: profile.ID, Type: resellerdomain.LedgerTypeOrderProfit, Currency: "CNY", Amount: money.FromDecimal(decimal.NewFromInt(100)), Status: resellerdomain.LedgerStatusLocked, IdempotencyKey: "locked"},
				{ResellerID: profile.ID, Type: resellerdomain.LedgerTypeOrderProfit, Currency: "USD", Amount: money.FromDecimal(decimal.NewFromInt(100)), Status: resellerdomain.LedgerStatusAvailable, IdempotencyKey: "usd"},
			}
			if err := db.Create(&rows).Error; err != nil {
				t.Fatal(err)
			}
			status := resellerdomain.BalanceStatusNormal
			if tc.frozen {
				status = resellerdomain.BalanceStatusFrozenReview
			}
			// Deliberately stale cache: the ledger, not this display value, is authoritative.
			if err := db.Create(&resellerdomain.BalanceAccount{ResellerID: profile.ID, Currency: "CNY", Status: status, AvailableAmountCache: money.FromDecimal(decimal.NewFromInt(999))}).Error; err != nil {
				t.Fatal(err)
			}
			svc := resellerapp.NewAccountingWithdrawService(resellergormstore.New(db))
			req, err := svc.ApplyWithdraw(profile.ID, resellercontract.WithdrawApplyInput{Amount: decimal.RequireFromString(tc.request), Currency: " cny ", Channel: "manual", Account: "test"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			var count int64
			if err := db.Model(&resellerdomain.WithdrawRequest{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if tc.want != nil {
				if count != 0 {
					t.Fatal("rejected request changed withdrawal records")
				}
				var after resellerdomain.LedgerEntry
				if err := db.First(&after, rows[0].ID).Error; err != nil {
					t.Fatal(err)
				}
				if after.Status != resellerdomain.LedgerStatusAvailable || !after.Amount.Decimal.Equal(rows[0].Amount.Decimal) {
					t.Fatal("rejected request mutated available profit")
				}
				return
			}
			if count != 1 || req == nil || req.Status != resellerdomain.WithdrawStatusPending {
				t.Fatal("expected one pending withdrawal")
			}
			// A new request rechecks the remaining available balance after the first lock.
			_, err = svc.ApplyWithdraw(profile.ID, resellercontract.WithdrawApplyInput{Amount: decimal.NewFromInt(1), Currency: "CNY", Channel: "manual", Account: "test"})
			if !errors.Is(err, resellercontract.ErrWithdrawBalanceBelowMinimum) {
				t.Fatalf("remaining balance must be rechecked, got %v", err)
			}
		})
	}
}
