// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

type refundAllocationItem struct {
	OrderItemID          string `json:"order_item_id"`
	ChildOrderID         string `json:"child_order_id,omitempty"`
	RefundRatio          string `json:"refund_ratio"`
	OriginalProfitAmount string `json:"original_profit_amount"`
	DeductAmount         string `json:"deduct_amount"`
}

type refundAllocation struct {
	RefundRecordID uint                   `json:"refund_record_id"`
	OrderID        uint                   `json:"order_id"`
	RefundAmount   string                 `json:"refund_amount"`
	OrderAmount    string                 `json:"order_amount"`
	Items          []refundAllocationItem `json:"items"`
}

func decimalFromSnapshotValue(v interface{}) decimal.Decimal {
	switch val := v.(type) {
	case string:
		d, err := decimal.NewFromString(strings.TrimSpace(val))
		if err == nil {
			return d.Round(2)
		}
	case float64:
		return decimal.NewFromFloat(val).Round(2)
	case int:
		return decimal.NewFromInt(int64(val)).Round(2)
	case int64:
		return decimal.NewFromInt(val).Round(2)
	case uint:
		return decimal.NewFromUint64(uint64(val)).Round(2)
	case uint64:
		return decimal.NewFromUint64(val).Round(2)
	case decimal.Decimal:
		return val.Round(2)
	}
	return decimal.Zero
}

func stringFromSnapshotValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case float64:
		return decimal.NewFromFloat(val).StringFixed(0)
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case uint:
		return fmt.Sprintf("%d", val)
	case uint64:
		return fmt.Sprintf("%d", val)
	}
	return ""
}

// HandleRefundDeduct 在调用方已开启的事务 store 上写入退款利润扣减流水。
func (s *AccountingLedgerService) HandleRefundDeduct(
	store resellercontract.AccountingLedgerStore,
	order *orderdomain.Order,
	refundRecord *orderdomain.OrderRefundRecord,
	refundedBefore decimal.Decimal,
) error {
	if s == nil || store == nil || order == nil || refundRecord == nil || refundRecord.ID == 0 {
		return nil
	}
	if order.ResellerID == nil || *order.ResellerID == 0 {
		return nil
	}
	refundAmount := refundRecord.Amount.Decimal.Round(2)
	if refundAmount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	ledgerOrderID := order.ID
	if order.ParentID != nil {
		ledgerOrderID = *order.ParentID
	}
	snapshot, err := store.GetOrderSnapshotByOrderID(ledgerOrderID)
	if err != nil {
		return err
	}
	if snapshot == nil {
		logger.Warnw("reseller_refund_missing_snapshot_skip", "order_id", order.ID, "order_no", order.OrderNo, "refund_record_id", refundRecord.ID)
		return nil
	}
	if !snapshot.ProfitEligible {
		return nil
	}
	profit := snapshot.ProfitAmount.Decimal.Round(2)
	if profit.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	// Checkout snapshots/profit credits belong to the parent order. A refund of
	// one child must debit that parent's ledger using only this child's margin.
	scopeProfit := profit
	snapshotItems := refundSnapshotItems(snapshot)
	scopedItems := snapshotItems
	orderAmount := snapshot.ResellerAmount.Decimal.Round(2)
	if order.ParentID != nil {
		parent, err := store.GetSettlementOrderForUpdate(ledgerOrderID)
		if err != nil {
			return err
		}
		if parent == nil || parent.ResellerID == nil || *parent.ResellerID != snapshot.ResellerID || snapshot.ResellerID != *order.ResellerID {
			return resellercontract.ErrLedgerInvalidSnapshot
		}
		scopeProfit = decimal.Zero
		scopedItems = nil
		for _, item := range snapshotItems {
			if stringFromSnapshotValue(item["child_order_id"]) == fmt.Sprint(order.ID) {
				scopedItems = append(scopedItems, item)
				scopeProfit = scopeProfit.Add(decimalFromSnapshotValue(item["profit_amount"]))
			}
		}
		if len(scopedItems) == 0 {
			return resellercontract.ErrLedgerInvalidSnapshot
		}
		if scopeProfit.LessThanOrEqual(decimal.Zero) {
			return nil
		}
		orderAmount = order.TotalAmount.Decimal.Round(2)
	}
	if orderAmount.LessThanOrEqual(decimal.Zero) {
		orderAmount = order.TotalAmount.Decimal.Round(2)
	}
	if orderAmount.LessThanOrEqual(decimal.Zero) {
		return resellercontract.ErrLedgerInvalidSnapshot
	}
	refundedBefore = refundedBefore.Round(2)
	remainingBefore := orderAmount.Sub(refundedBefore).Round(2)
	if remainingBefore.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	if refundAmount.GreaterThan(remainingBefore) {
		refundAmount = remainingBefore
	}
	currency := strings.TrimSpace(snapshot.Currency)
	if currency == "" {
		currency = strings.TrimSpace(refundRecord.Currency)
	}
	if currency == "" {
		currency = strings.TrimSpace(order.Currency)
	}
	if currency == "" {
		return resellercontract.ErrLedgerInvalidSnapshot
	}
	// Serialize deductions with confirmation and withdrawal before reading totals.
	if _, err := store.GetOrCreateBalanceAccountForUpdate(snapshot.ResellerID, currency); err != nil {
		return err
	}
	deductedSoFar, err := store.SumLedgerAmountByOrderAndType(ledgerOrderID, resellerdomain.LedgerTypeRefundDeduct)
	if err != nil {
		return err
	}
	remainingProfit := profit.Sub(deductedSoFar.Abs().Round(2)).Round(2)
	if remainingProfit.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	remainingScopeProfit := remainingProfit
	if order.ParentID != nil {
		previous, err := store.ListOrderLedgerEntriesForUpdate(ledgerOrderID)
		if err != nil {
			return err
		}
		deductedChild := decimal.Zero
		childItemIDs := make(map[string]bool)
		for _, item := range scopedItems {
			childItemIDs[stringFromSnapshotValue(item["order_item_id"])] = true
		}
		for _, prior := range previous {
			if prior.Type != resellerdomain.LedgerTypeRefundDeduct {
				continue
			}
			var allocation refundAllocation
			encoded, err := json.Marshal(prior.MetadataJSON["refund_allocation_json"])
			if err != nil {
				return err
			}
			if err := json.Unmarshal(encoded, &allocation); err != nil {
				return resellercontract.ErrLedgerInvalidSnapshot
			}
			if len(allocation.Items) == 0 {
				// Old root debits without per-item allocation still reduce each
				// child's remaining margin proportionally; never ignore them.
				deductedChild = deductedChild.Add(prior.Amount.Decimal.Abs().Mul(scopeProfit).Div(profit).Round(2))
				continue
			}
			for _, item := range allocation.Items {
				if item.ChildOrderID == fmt.Sprint(order.ID) || (item.ChildOrderID == "" && childItemIDs[item.OrderItemID]) {
					deductedChild = deductedChild.Add(decimalFromSnapshotValue(item.DeductAmount))
				}
			}
		}
		remainingScopeProfit = scopeProfit.Sub(deductedChild).Round(2)
		if remainingScopeProfit.LessThanOrEqual(decimal.Zero) {
			return nil
		}
	}
	ratio := refundAmount.Div(orderAmount)
	deduct := scopeProfit.Mul(ratio).Round(2)
	fullyRefunded := refundedBefore.Add(refundAmount).GreaterThanOrEqual(orderAmount)
	if fullyRefunded || deduct.GreaterThan(remainingScopeProfit) {
		deduct = remainingScopeProfit
	}
	if deduct.GreaterThan(remainingProfit) {
		deduct = remainingProfit
	}
	if deduct.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	allocRatio := deduct.Div(scopeProfit)
	allocation := refundAllocation{
		RefundRecordID: refundRecord.ID,
		OrderID:        order.ID,
		RefundAmount:   refundAmount.StringFixed(2),
		OrderAmount:    orderAmount.StringFixed(2),
		Items:          make([]refundAllocationItem, 0),
	}
	for _, itemMap := range scopedItems {
		itemProfit := decimalFromSnapshotValue(itemMap["profit_amount"])
		itemDeduct := itemProfit.Mul(allocRatio).Round(2)
		if itemDeduct.LessThanOrEqual(decimal.Zero) {
			continue
		}
		allocation.Items = append(allocation.Items, refundAllocationItem{
			OrderItemID:          stringFromSnapshotValue(itemMap["order_item_id"]),
			ChildOrderID:         stringFromSnapshotValue(itemMap["child_order_id"]),
			RefundRatio:          ratio.StringFixed(8),
			OriginalProfitAmount: itemProfit.StringFixed(2),
			DeductAmount:         itemDeduct.StringFixed(2),
		})
	}
	now := time.Now()
	orderID := ledgerOrderID

	deductStatus := resellerdomain.LedgerStatusAvailable
	var deductAvailableAt *time.Time
	confirmationMode := s.confirmationMode
	var deliveryCompletedAt *time.Time
	profitEntry, err := store.GetLedgerEntryByIdempotencyKey(fmt.Sprintf("order_profit:%d", ledgerOrderID))
	if err != nil {
		return err
	}
	if profitEntry != nil {
		confirmationMode = profitEntry.ConfirmationMode
		deliveryCompletedAt = profitEntry.DeliveryCompletedAt
	}
	if profitEntry != nil && profitEntry.Status == resellerdomain.LedgerStatusPendingConfirm {
		deductStatus = resellerdomain.LedgerStatusPendingConfirm
		if profitEntry.AvailableAt != nil {
			deductAvailableAt = profitEntry.AvailableAt
		}
	}

	entry := &resellerdomain.LedgerEntry{
		ResellerID:          snapshot.ResellerID,
		OrderID:             &orderID,
		Type:                resellerdomain.LedgerTypeRefundDeduct,
		Amount:              money.FromDecimal(deduct.Neg()),
		Currency:            currency,
		ConfirmationMode:    confirmationMode,
		DeliveryCompletedAt: deliveryCompletedAt,
		Status:              deductStatus,
		AvailableAt:         deductAvailableAt,
		MetadataJSON: jsonmap.JSON{
			"refund_record_id":       refundRecord.ID,
			"refund_order_id":        order.ID,
			"refund_type":            refundRecord.Type,
			"refund_amount":          refundAmount.StringFixed(2),
			"refunded_before":        refundedBefore.Round(2).StringFixed(2),
			"refund_allocation_json": allocation,
			"snapshot_id":            snapshot.ID,
			"deduct_status":          deductStatus,
		},
		IdempotencyKey: fmt.Sprintf("refund_deduct:%d", refundRecord.ID),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if entry.Currency == "" {
		entry.Currency = strings.TrimSpace(refundRecord.Currency)
	}
	if entry.Currency == "" {
		entry.Currency = strings.TrimSpace(order.Currency)
	}
	if entry.Currency == "" {
		return resellercontract.ErrLedgerInvalidSnapshot
	}
	_, err = store.CreateLedgerEntryIfNotExists(entry)
	if err != nil {
		return err
	}
	return RefreshBalanceAccount(store, snapshot.ResellerID, entry.Currency, now)
}

// Normalize JSON-backed snapshot arrays as well as in-memory jsonmap fixtures.
func refundSnapshotItems(snapshot *resellerdomain.OrderSnapshot) []map[string]interface{} {
	if snapshot == nil {
		return nil
	}
	encoded, err := json.Marshal(snapshot.PricingSnapshotJSON["items"])
	if err != nil {
		return nil
	}
	var items []map[string]interface{}
	if json.Unmarshal(encoded, &items) != nil {
		return nil
	}
	return items
}
