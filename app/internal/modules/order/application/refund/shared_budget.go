// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package refund

import (
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

// lockRefundOrderAndCheckBudget serializes every refund of one payment on its
// root order, before locking a child or crediting a wallet. A parent's status
// can be propagated to children without propagating refunded_amount, so status
// and any one row's cached amount cannot prove the payment's remaining budget.
func lockRefundOrderAndCheckBudget(store ordercontract.Store, orderID uint, amount decimal.Decimal) (*orderdomain.Order, error) {
	initial, err := store.GetByID(orderID)
	if err != nil {
		return nil, err
	}
	if initial == nil {
		return nil, ErrOrderNotFound
	}
	rootID := initial.ID
	if initial.ParentID != nil {
		rootID = *initial.ParentID
	}
	root, err := store.GetByIDForUpdate(rootID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.ParentID != nil {
		return nil, ErrOrderNotFound
	}
	order := root
	if orderID != rootID {
		order, err = store.GetByIDForUpdate(orderID)
		if err != nil {
			return nil, err
		}
		if order == nil || order.ParentID == nil || *order.ParentID != rootID {
			return nil, ErrOrderNotFound
		}
	}
	if root.PaidAt == nil || !root.TotalAmount.Decimal.IsPositive() || order.Currency != root.Currency || order.UserID != root.UserID {
		return nil, ErrOrderStatusInvalid
	}
	children, err := store.ListChildren(rootID)
	if err != nil {
		return nil, err
	}
	ids := []uint{rootID}
	childAmountCache := decimal.Zero
	for _, child := range children {
		ids = append(ids, child.ID)
		childAmountCache = childAmountCache.Add(child.RefundedAmount.Decimal)
	}
	records, err := store.ListRefundRecordsByOrderIDs(ids)
	if err != nil {
		return nil, err
	}
	refundedTotal, refundedTarget := decimal.Zero, decimal.Zero
	parentRefunds, childRefunds := decimal.Zero, decimal.Zero
	for _, record := range records {
		if record.Currency != root.Currency || record.Amount.Decimal.IsNegative() {
			return nil, ErrOrderStatusInvalid
		}
		refundedTotal = refundedTotal.Add(record.Amount.Decimal)
		if record.OrderID == rootID {
			parentRefunds = parentRefunds.Add(record.Amount.Decimal)
		} else {
			childRefunds = childRefunds.Add(record.Amount.Decimal)
		}
		if record.OrderID == order.ID {
			refundedTarget = refundedTarget.Add(record.Amount.Decimal)
		}
	}
	// Records are authoritative across the tree. Old imported orders can have
	// refunded_amount without records: never increase their existing allowance.
	// Take maxima, not root+children, because a root cache may be aggregated.
	refundedTotal = decimal.Max(refundedTotal, root.RefundedAmount.Decimal, childAmountCache).Round(2)
	refundedTarget = decimal.Max(refundedTarget, order.RefundedAmount.Decimal).Round(2)
	if amount.GreaterThan(root.TotalAmount.Decimal.Sub(refundedTotal).Round(2)) ||
		amount.GreaterThan(order.TotalAmount.Decimal.Sub(refundedTarget).Round(2)) {
		return nil, walletcontract.ErrRefundExceeded
	}
	// Parent refunds have no per-child principal allocation. Mixing them with
	// item refunds would make both item allowances and margin reversals
	// ambiguous. Keep the initial scope; manual vs wallet may still be mixed.
	hasParentRefund := parentRefunds.IsPositive() || (refundedTotal.IsPositive() && root.RefundedAmount.Decimal.IsPositive() && !childRefunds.IsPositive())
	hasChildRefund := childRefunds.IsPositive() || childAmountCache.IsPositive()
	if (order.ID == rootID && hasChildRefund) || (order.ID != rootID && hasParentRefund) {
		return nil, ErrOrderRefundScopeConflict
	}
	// Recover a stale low amount cache from immutable refund records before the
	// caller writes its next cumulative amount and computes refund deductions.
	order.RefundedAmount = money.FromDecimal(refundedTarget)
	return order, nil
}
