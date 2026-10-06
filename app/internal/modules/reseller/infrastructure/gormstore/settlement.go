// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package gormstore

import (
	"errors"
	"time"

	"github.com/dujiao-next/internal/constants"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) GetLedgerEntryByID(id uint) (*resellerdomain.LedgerEntry, error) {
	var row resellerdomain.LedgerEntry
	err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

// Lock order before its balance account, matching payment and refund transactions.
func (s *Store) GetSettlementOrderForUpdate(id uint) (*orderdomain.Order, error) {
	var row orderdomain.Order
	err := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND deleted_at IS NULL", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

// Delivery is proved by actual fulfillment records, never by a mutable order status.
// A backdated manual delivered_at cannot shorten the hold below the server record creation time.
func (s *Store) GetOrderDeliveryCompletedAt(orderID uint) (*time.Time, error) {
	var children []orderdomain.Order
	if err := s.db.Where("parent_id = ?", orderID).Find(&children).Error; err != nil {
		return nil, err
	}
	ids := []uint{orderID}
	if len(children) > 0 {
		ids = nil
		for _, child := range children {
			if child.DeletedAt != nil {
				return nil, nil
			}
			// Fully refunded, undelivered items have no remaining delivery
			// obligation. Status alone is insufficient (parent refund propagation
			// can set it); the child's recorded refunded amount must cover it.
			if child.Status == constants.OrderStatusRefunded && child.TotalAmount.Decimal.IsPositive() && child.RefundedAmount.Decimal.GreaterThanOrEqual(child.TotalAmount.Decimal) {
				continue
			}
			ids = append(ids, child.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var records []fulfillmentdomain.Fulfillment
	if err := s.db.Where("order_id IN ? AND deleted_at IS NULL", ids).Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) != len(ids) {
		return nil, nil
	}
	var completed time.Time
	for _, record := range records {
		if record.Status != constants.FulfillmentStatusDelivered || record.DeliveredAt == nil || record.CreatedAt.IsZero() {
			return nil, nil
		}
		at := *record.DeliveredAt
		if record.CreatedAt.After(at) {
			at = record.CreatedAt
		}
		if at.After(completed) {
			completed = at
		}
	}
	if completed.IsZero() {
		return nil, nil
	}
	return &completed, nil
}

func (s *Store) ListPendingProfitEntries() ([]resellerdomain.LedgerEntry, error) {
	var rows []resellerdomain.LedgerEntry
	err := s.db.Where("type = ? AND status = ? AND confirmed_at IS NULL AND deleted_at IS NULL", resellerdomain.LedgerTypeOrderProfit, resellerdomain.LedgerStatusPendingConfirm).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (s *Store) ListOrderLedgerEntriesForUpdate(orderID uint) ([]resellerdomain.LedgerEntry, error) {
	var rows []resellerdomain.LedgerEntry
	err := s.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND deleted_at IS NULL", orderID).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (s *Store) SumLockedLedgerByWithdrawID(id uint) (decimal.Decimal, error) {
	var total decimal.Decimal
	err := s.db.Model(&resellerdomain.LedgerEntry{}).Where("withdraw_request_id = ? AND status = ? AND deleted_at IS NULL", id, resellerdomain.LedgerStatusLocked).Select("COALESCE(SUM(amount),0)").Scan(&total).Error
	return total.Round(2), err
}

// IsOrderAutomaticSettlementEligible is used after locking the root order and
// balance. Refunds of a child serialize through the root before ledger mutation.
// Status alone cannot prove delivery; callers also require actual fulfillment.
func (s *Store) IsOrderAutomaticSettlementEligible(orderID uint) (bool, error) {
	var orders []orderdomain.Order
	if err := s.db.Where("id = ? OR parent_id = ?", orderID, orderID).Find(&orders).Error; err != nil {
		return false, err
	}
	ids := make([]uint, 0, len(orders))
	foundRoot := false
	for _, order := range orders {
		if order.ID == orderID {
			foundRoot = true
		}
		if order.DeletedAt != nil || order.RefundedAmount.Decimal.IsPositive() ||
			(order.Status != constants.OrderStatusCompleted && order.Status != constants.OrderStatusDelivered) {
			return false, nil
		}
		ids = append(ids, order.ID)
	}
	if !foundRoot {
		return false, nil
	}
	var refunds int64
	if err := s.db.Model(&orderdomain.OrderRefundRecord{}).Where("order_id IN ? AND amount > 0 AND deleted_at IS NULL", ids).Count(&refunds).Error; err != nil {
		return false, err
	}
	if refunds != 0 {
		return false, nil
	}
	var debits, credits int64
	if err := s.db.Model(&resellerdomain.LedgerEntry{}).Where("order_id = ? AND type = ? AND deleted_at IS NULL", orderID, resellerdomain.LedgerTypeRefundDeduct).Count(&debits).Error; err != nil {
		return false, err
	}
	if err := s.db.Model(&resellerdomain.LedgerEntry{}).Where("order_id = ? AND type = ? AND deleted_at IS NULL", orderID, resellerdomain.LedgerTypeOrderProfit).Count(&credits).Error; err != nil {
		return false, err
	}
	return debits == 0 && credits == 1, nil
}
