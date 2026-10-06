// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package gormstore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	dashboard "github.com/dujiao-next/internal/modules/dashboard/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

type productRefundRow struct {
	dashboard.ProductRankingRow
	RefundRecordID       uint
	OrderItemID          uint
	CommissionWeight     money.Amount
	RefundLedgerAmount   money.Amount
	RefundLedgerMetadata jsonmap.JSON `gorm:"type:json"`
}

// Refunds belong to their occurrence period. A parent refund is allocated over
// its children's goods lines because the parent itself has no order items.
func (r *Store) getProductRefundAdjustments(startAt, endAt time.Time) ([]dashboard.ProductRankingRow, error) {
	lines := make([]productRefundRow, 0)
	titleExpr := localizedJSONCoalesceExpr(r.db, "order_items.title_json")
	timeWhere, args := timeRangeQuery(r.db, "order_refund_records.created_at", startAt, endAt)
	ratio := "(1.0 * order_refund_records.amount / refund_order.total_amount)"
	err := r.db.Model(&orderdomain.OrderRefundRecord{}).
		Select(fmt.Sprintf(`
			order_refund_records.id as refund_record_id,
			order_items.id as order_item_id,
			order_items.product_id as product_id,
			order_items.sku_id as sku_id,
			COALESCE(product_skus.sku_code, '') as sku_code,
			product_skus.spec_values_json as sku_spec_values_json,
			%s as title,
			-(order_items.total_price - order_items.coupon_discount) * %s as paid_amount,
			order_items.cost_price * order_items.quantity * %s as refunded_cost,
			%s as commission_weight,
			COALESCE(refund_ledger.amount, 0) as refund_ledger_amount,
			refund_ledger.metadata_json as refund_ledger_metadata
		`, titleExpr, ratio, ratio, resellerProfitShareExpr)).
		Joins("JOIN orders refund_order ON refund_order.id = order_refund_records.order_id AND refund_order.deleted_at IS NULL AND refund_order.total_amount > 0").
		Joins("JOIN orders ON (orders.id = refund_order.id OR orders.parent_id = refund_order.id) AND orders.deleted_at IS NULL").
		Joins("JOIN order_items ON order_items.order_id = orders.id AND order_items.deleted_at IS NULL").
		Joins("LEFT JOIN product_skus ON product_skus.id = order_items.sku_id AND product_skus.deleted_at IS NULL").
		Joins(r.refundLedgerJoin()).
		Where("order_refund_records.deleted_at IS NULL AND "+timeWhere, args...).
		Order("order_refund_records.id, order_items.id").
		Scan(&lines).Error
	if err != nil {
		return nil, err
	}
	rows := make([]dashboard.ProductRankingRow, 0, len(lines))
	for first := 0; first < len(lines); {
		last := first + 1
		for last < len(lines) && lines[last].RefundRecordID == lines[first].RefundRecordID {
			last++
		}
		allocateRefundLedgerAmount(lines[first:last])
		for _, line := range lines[first:last] {
			rows = append(rows, line.ProductRankingRow)
		}
		first = last
	}
	return rows, nil
}

// Metadata preserves the wallet's item allocation. Normalize its weights to
// the actual ledger debit and distribute any cent remainder deterministically:
// historical metadata may itself have rounded each item independently.
func allocateRefundLedgerAmount(lines []productRefundRow) {
	if len(lines) == 0 || !lines[0].RefundLedgerAmount.Decimal.IsNegative() {
		return
	}
	totalCents := lines[0].RefundLedgerAmount.Decimal.Abs().Round(2).Mul(decimal.NewFromInt(100)).IntPart()
	var allocation struct {
		Items []struct {
			OrderItemID  string `json:"order_item_id"`
			DeductAmount string `json:"deduct_amount"`
		} `json:"items"`
	}
	encoded, _ := json.Marshal(lines[0].RefundLedgerMetadata["refund_allocation_json"])
	_ = json.Unmarshal(encoded, &allocation)
	metadataWeights := make(map[uint]decimal.Decimal, len(allocation.Items))
	for _, item := range allocation.Items {
		id, idErr := strconv.ParseUint(item.OrderItemID, 10, 64)
		weight, err := decimal.NewFromString(item.DeductAmount)
		if idErr == nil && err == nil && weight.IsPositive() {
			metadataWeights[uint(id)] = weight
		}
	}
	weights := make([]decimal.Decimal, len(lines))
	sum := decimal.Zero
	for i, line := range lines {
		weights[i] = metadataWeights[line.OrderItemID]
		sum = sum.Add(weights[i])
	}
	if !sum.IsPositive() {
		for i, line := range lines {
			weights[i] = decimal.Max(line.CommissionWeight.Decimal, decimal.Zero)
			sum = sum.Add(weights[i])
		}
	}
	if !sum.IsPositive() {
		for i, line := range lines {
			weights[i] = decimal.NewFromFloat(line.PaidAmount).Abs()
			sum = sum.Add(weights[i])
		}
	}
	if !sum.IsPositive() {
		weights[0], sum = decimal.NewFromInt(1), decimal.NewFromInt(1)
	}
	cents := make([]int64, len(lines))
	fractions := make([]decimal.Decimal, len(lines))
	indices := make([]int, len(lines))
	remaining := totalCents
	for i, weight := range weights {
		exact := decimal.NewFromInt(totalCents).Mul(weight).Div(sum)
		cents[i] = exact.IntPart()
		fractions[i] = exact.Sub(decimal.NewFromInt(cents[i]))
		indices[i] = i
		remaining -= cents[i]
	}
	sort.SliceStable(indices, func(i, j int) bool { return fractions[indices[i]].GreaterThan(fractions[indices[j]]) })
	for i := int64(0); i < remaining; i++ {
		cents[indices[int(i)%len(indices)]]++
	}
	for i := range lines {
		lines[i].ResellerProfit = -float64(cents[i]) / 100
	}
}
