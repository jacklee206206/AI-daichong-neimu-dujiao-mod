// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package gormstore

import (
	"time"

	"github.com/dujiao-next/internal/constants"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
)

var _ procurementcontract.SubmissionRecoveryRepository = (*Store)(nil)

func (s *Store) ListDueSubmissions(now time.Time, afterID uint, limit int) ([]procurementdomain.Order, error) {
	var orders []procurementdomain.Order
	err := s.active().Model(&procurementdomain.Order{}).
		Where("id > ? AND upstream_order_id = 0 AND (upstream_order_no IS NULL OR upstream_order_no = '')", afterID).
		Where("(status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)) OR (status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?)",
			procurementdomain.StatusPending, now, procurementdomain.StatusFailed, now).
		Order("id ASC").Limit(limit).Find(&orders).Error
	return orders, err
}

func (s *Store) MarkStaleSubmissionsUnknown(before, now time.Time) (int64, error) {
	result := s.active().Model(&procurementdomain.Order{}).
		Where("status = ? AND updated_at < ?", procurementdomain.StatusSubmitting, before).
		Updates(map[string]interface{}{
			"status":        procurementdomain.StatusUnknown,
			"next_retry_at": nil,
			"updated_at":    now,
			"error_message": "采购请求执行中断，扣款结果待核对；为避免重复扣款，系统不会自动重新采购。",
		})
	return result.RowsAffected, result.Error
}

// ListPaidOrdersMissingProcurement repairs the gap between payment commit and
// procurement creation. Read the immutable order-item fulfillment snapshot;
// today's product fulfillment mode must never turn old auto/manual sales into
// supplier purchases. One item is required because Submit currently buys only
// that item. Historical purchases, deliveries and refunds remain exclusions
// even when an administrator has soft-deleted their local records.
func (s *Store) ListPaidOrdersMissingProcurement(afterID uint, limit int) ([]uint, error) {
	var ids []uint
	err := s.db.Table("orders AS o").
		Where("o.id > ? AND o.deleted_at IS NULL AND o.status IN ? AND o.refunded_amount = 0", afterID,
			[]string{constants.OrderStatusPaid, constants.OrderStatusFulfilling}).
		Where("NOT EXISTS (SELECT 1 FROM orders child WHERE child.parent_id = o.id)").
		Where("NOT EXISTS (SELECT 1 FROM procurement_orders p WHERE p.local_order_id = o.id)").
		Where("NOT EXISTS (SELECT 1 FROM fulfillments f WHERE f.order_id = o.id)").
		Where("NOT EXISTS (SELECT 1 FROM order_refund_records r WHERE r.order_id = o.id)").
		Where(`(o.parent_id IS NULL OR EXISTS (
			SELECT 1 FROM orders parent WHERE parent.id = o.parent_id
			AND parent.deleted_at IS NULL AND parent.refunded_amount = 0
			AND parent.status IN ?
			AND NOT EXISTS (SELECT 1 FROM order_refund_records pr WHERE pr.order_id = parent.id)
		))`, []string{constants.OrderStatusPaid, constants.OrderStatusFulfilling, constants.OrderStatusPartiallyDelivered}).
		Where("(SELECT COUNT(*) FROM order_items oi WHERE oi.order_id = o.id AND oi.deleted_at IS NULL) = 1").
		Where(`EXISTS (
			SELECT 1 FROM order_items oi
			JOIN product_mappings pm ON pm.local_product_id = oi.product_id
			JOIN sku_mappings sm ON sm.product_mapping_id = pm.id AND sm.local_sku_id = oi.sku_id
			JOIN site_connections sc ON sc.id = pm.connection_id
			WHERE oi.order_id = o.id AND oi.deleted_at IS NULL AND oi.fulfillment_type = ? AND oi.quantity > 0
			AND pm.deleted_at IS NULL AND pm.is_active = ? AND pm.upstream_status = ? AND pm.upstream_product_id > 0
			AND sm.deleted_at IS NULL AND sm.upstream_is_active = ? AND sm.upstream_sku_id > 0
			AND sc.deleted_at IS NULL AND sc.status = ?
		)`, constants.FulfillmentTypeUpstream, true, "active", true, constants.ConnectionStatusActive).
		Order("o.id ASC").Limit(limit).Pluck("o.id", &ids).Error
	return ids, err
}
