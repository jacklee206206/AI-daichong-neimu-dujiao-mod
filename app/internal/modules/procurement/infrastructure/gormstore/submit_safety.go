// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package gormstore

import (
	"fmt"
	"time"

	"github.com/dujiao-next/internal/constants"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ procurementcontract.SubmissionRepository = (*Store)(nil)

func (s *Store) ClaimSubmission(id uint, now time.Time) (bool, error) {
	result := s.db.Model(&procurementdomain.Order{}).
		Where("id = ? AND deleted_at IS NULL AND status IN ? AND upstream_order_id = 0", id, []string{"pending", "failed"}).
		Where("upstream_order_no IS NULL OR upstream_order_no = ''").
		Where("next_retry_at IS NULL OR next_retry_at <= ?", now).
		Where("EXISTS (SELECT 1 FROM orders WHERE orders.id = procurement_orders.local_order_id AND orders.deleted_at IS NULL AND orders.status IN ? AND orders.refunded_amount = 0)", []string{constants.OrderStatusPaid, constants.OrderStatusFulfilling}).
		Where("NOT EXISTS (SELECT 1 FROM order_refund_records r WHERE r.order_id = procurement_orders.local_order_id)").
		Where(`EXISTS (SELECT 1 FROM orders child WHERE child.id = procurement_orders.local_order_id AND
			(child.parent_id IS NULL OR EXISTS (SELECT 1 FROM orders parent WHERE parent.id = child.parent_id
			AND parent.deleted_at IS NULL AND parent.refunded_amount = 0 AND parent.status IN ?
			AND NOT EXISTS (SELECT 1 FROM order_refund_records r WHERE r.order_id = parent.id))))`,
			[]string{constants.OrderStatusPaid, constants.OrderStatusFulfilling, constants.OrderStatusPartiallyDelivered}).
		Updates(map[string]interface{}{"status": procurementdomain.StatusSubmitting, "next_retry_at": nil, "updated_at": now})
	return result.RowsAffected == 1, result.Error
}

func (s *Store) CompareAndSwapStatus(id uint, expected []string, status string, updates map[string]interface{}) (bool, error) {
	values := make(map[string]interface{}, len(updates)+1)
	for key, value := range updates {
		values[key] = value
	}
	values["status"] = status
	query := s.db.Model(&procurementdomain.Order{}).Where("id = ? AND deleted_at IS NULL AND status IN ?", id, expected)
	// Known upstream orders must never be reset into the purchase queue.
	if status == "pending" || status == "failed" {
		query = query.Where("upstream_order_id = 0").Where("upstream_order_no IS NULL OR upstream_order_no = ''")
	}
	result := query.Updates(values)
	return result.RowsAffected == 1, result.Error
}

func (s *Store) RecordSubmissionAccepted(id uint, response *procurementcontract.CreateOrderResult, now time.Time) (bool, error) {
	accepted := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var current procurementdomain.Order
		if err := tx.Where("id = ? AND deleted_at IS NULL", id).First(&current).Error; err != nil {
			return err
		}
		var local orderdomain.Order
		if err := tx.Where("id = ? AND deleted_at IS NULL", current.LocalOrderID).First(&local).Error; err != nil {
			return err
		}
		// Match delivery's parent -> child -> procurement lock order.
		if local.ParentID != nil {
			var parent orderdomain.Order
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", *local.ParentID).First(&parent).Error; err != nil {
				return err
			}
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", local.ID).First(&local).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND deleted_at IS NULL", id).First(&current).Error; err != nil {
			return err
		}
		if current.UpstreamOrderID != 0 && current.UpstreamOrderID != response.OrderID {
			return fmt.Errorf("upstream order identity changed")
		}
		updates := map[string]interface{}{
			"upstream_order_id": response.OrderID, "upstream_order_no": response.OrderNo,
			"upstream_amount": response.Amount, "upstream_currency": response.Currency,
			"updated_at": now,
		}
		if current.Status == procurementdomain.StatusSubmitting {
			updates["status"] = constants.ProcurementStatusAccepted
			updates["error_message"] = ""
			updates["retry_count"] = 0
			updates["next_retry_at"] = nil
			accepted = true
		}
		// A callback can arrive before CreateOrder returns. Store its identity but
		// never move a delivered, canceled or refunded procurement backwards.
		if err := tx.Model(&procurementdomain.Order{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		if accepted {
			return tx.Model(&orderdomain.Order{}).
				Where("id = ? AND status = ? AND refunded_amount = 0 AND deleted_at IS NULL", current.LocalOrderID, constants.OrderStatusPaid).
				Updates(map[string]interface{}{"status": constants.OrderStatusFulfilling, "updated_at": now}).Error
		}
		return nil
	})
	return accepted, err
}

func (s *Store) UpdateLocalStatusIf(id uint, expected, status string, now time.Time) (bool, error) {
	result := s.db.Model(&orderdomain.Order{}).
		Where("id = ? AND status = ? AND refunded_amount = 0 AND deleted_at IS NULL", id, expected).
		Updates(map[string]interface{}{"status": status, "updated_at": now})
	return result.RowsAffected == 1, result.Error
}
