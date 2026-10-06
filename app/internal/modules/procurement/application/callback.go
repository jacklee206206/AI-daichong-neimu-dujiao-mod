// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"fmt"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/logger"
	procurementcontract "github.com/dujiao-next/internal/modules/procurement/contract"
	procurementdomain "github.com/dujiao-next/internal/modules/procurement/domain"
)

// HandleUpstreamCallback commits fulfillment and related order states together.
func (s *Service) HandleUpstreamCallback(procurementOrderID uint, upstreamStatus string, fulfillment *procurementcontract.Fulfillment) error {
	lifecycle, ok := s.orderLifecycle.(procurementcontract.CallbackLifecycle)
	if !ok {
		return fmt.Errorf("transactional procurement callback lifecycle is unavailable")
	}
	upstreamStatus = strings.ToLower(strings.TrimSpace(upstreamStatus))
	now := time.Now()
	var procOrder *procurementdomain.Order
	var localOrder *procurementdomain.LocalOrder
	var parentStatus string
	changed := false

	err := lifecycle.WithinCallbackTransaction(procurementOrderID, func(tx procurementcontract.CallbackTransaction) error {
		procOrder = tx.ProcurementOrder()
		localOrder = tx.LocalOrder()
		// Recheck under locks: callbacks may race each other or the purchase response.
		if !isUpstreamTransitionAllowed(procOrder.Status, upstreamStatus) {
			return nil
		}
		switch upstreamStatus {
		case "delivered", "completed", "fulfilled":
			alreadyDelivered := localOrder.Status == constants.OrderStatusDelivered
			// Refunded or closed customer orders must not be reopened by a late delivery.
			switch localOrder.Status {
			case constants.OrderStatusCanceled, constants.OrderStatusRefunded,
				constants.OrderStatusPartiallyRefunded, constants.OrderStatusCompleted:
				return nil
			}
			switch tx.ParentStatus() {
			case constants.OrderStatusCanceled, constants.OrderStatusRefunded, constants.OrderStatusCompleted:
				return nil
			}
			if fulfillment == nil || strings.TrimSpace(fulfillment.Payload) == "" {
				return fmt.Errorf("upstream delivery contains no fulfillment payload")
			}
			if err := s.createUpstreamFulfillment(tx, fulfillment, now); err != nil {
				return fmt.Errorf("create upstream fulfillment: %w", err)
			}
			targetStatus := constants.ProcurementStatusFulfilled
			// A partial refund can precede delivery; retain its accounting state.
			if procOrder.Status == constants.ProcurementStatusPartiallyRefunded {
				targetStatus = constants.ProcurementStatusPartiallyRefunded
			}
			if err := tx.UpdateProcurementStatus(targetStatus, map[string]interface{}{
				"upstream_payload": fulfillment.Payload, "updated_at": now,
			}); err != nil {
				return fmt.Errorf("update procurement status: %w", err)
			}
			if err := tx.UpdateLocalOrderStatus(constants.OrderStatusDelivered, now); err != nil {
				return fmt.Errorf("update delivered order: %w", err)
			}
			var err error
			parentStatus, err = tx.SyncParentStatus(now)
			if err != nil {
				return fmt.Errorf("sync parent order: %w", err)
			}
			changed = !alreadyDelivered
		case "canceled":
			if err := tx.UpdateProcurementStatus(constants.ProcurementStatusCanceled, map[string]interface{}{"updated_at": now}); err != nil {
				return fmt.Errorf("update procurement status: %w", err)
			}
			if localOrder.Status == constants.OrderStatusFulfilling {
				if err := tx.UpdateLocalOrderStatus(constants.OrderStatusPaid, now); err != nil {
					return fmt.Errorf("restore paid order: %w", err)
				}
				if _, err := tx.SyncParentStatus(now); err != nil {
					return fmt.Errorf("sync parent order: %w", err)
				}
			}
			changed = true
		case "refunded", "partially_refunded":
			updates := map[string]interface{}{"updated_at": now}
			if fulfillment != nil && strings.TrimSpace(fulfillment.Payload) != "" {
				updates["upstream_payload"] = fulfillment.Payload
			}
			if err := tx.UpdateProcurementStatus(upstreamStatus, updates); err != nil {
				return fmt.Errorf("update procurement refund status: %w", err)
			}
			changed = true
		default:
			logger.Warnw("procurement_unknown_upstream_status", "procurement_order_id", procOrder.ID, "upstream_status", upstreamStatus)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	// External effects are emitted only after the entire database transaction commits.
	switch upstreamStatus {
	case "delivered", "completed", "fulfilled":
		notifyOrderID := localOrder.ID
		notifyStatus := constants.OrderStatusDelivered
		if localOrder.ParentID != nil {
			notifyOrderID = *localOrder.ParentID
			notifyStatus = parentStatus
		}
		if notifyStatus != "" && notifyStatus != constants.OrderStatusCanceled {
			if _, err := s.orderLifecycle.EnqueueStatusEmail(notifyOrderID, notifyStatus); err != nil {
				logger.Warnw("procurement_enqueue_status_email_failed", "order_id", notifyOrderID, "error", err)
			}
		}
		if s.downstreamCallback != nil {
			s.downstreamCallback.EnqueueCallback(localOrder.ID)
			if localOrder.ParentID != nil {
				s.downstreamCallback.EnqueueCallback(*localOrder.ParentID)
			}
		}
		if s.botNotifier != nil {
			go s.botNotifier.NotifyBotOrderFulfilled(localOrder.UserID, notifyOrderID)
		}
		logger.Infow("procurement_order_fulfilled", "procurement_order_id", procOrder.ID, "local_order_id", localOrder.ID)
	case "canceled":
		s.notifyProcurementFailure(procOrder, "upstream canceled order")
		logger.Infow("procurement_order_canceled_by_upstream", "procurement_order_id", procOrder.ID, "local_order_id", localOrder.ID)
	case "refunded", "partially_refunded":
		logger.Infow("procurement_order_refunded", "procurement_order_id", procOrder.ID, "local_order_id", localOrder.ID, "upstream_status", upstreamStatus)
	}
	return nil
}

func (s *Service) createUpstreamFulfillment(tx procurementcontract.CallbackTransaction, fulfillment *procurementcontract.Fulfillment, now time.Time) error {
	return tx.CreateUpstreamFulfillment(fulfillment, now)
}

// Preserve completed deliveries and refunds when callbacks are repeated or reordered.
func isUpstreamTransitionAllowed(current, upstreamStatus string) bool {
	if current == constants.ProcurementStatusRefunded {
		return false
	}
	if current == constants.ProcurementStatusPartiallyRefunded && upstreamStatus == "canceled" {
		return false
	}
	switch upstreamStatus {
	case "delivered", "completed", "fulfilled", "canceled":
		switch current {
		case constants.ProcurementStatusFulfilled, constants.ProcurementStatusCompleted,
			constants.ProcurementStatusCanceled:
			return false
		}
	case "partially_refunded":
		return current != constants.ProcurementStatusPartiallyRefunded
	}
	return true
}
