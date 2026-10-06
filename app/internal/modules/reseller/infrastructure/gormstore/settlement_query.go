// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package gormstore

import (
	"errors"
	"time"

	"github.com/dujiao-next/internal/constants"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"gorm.io/gorm"
)

// PopulateLedgerReviewState is read-only. The confirm mutation performs all checks
// again under transaction locks; a list response is not an approval capability.
func (s *Store) PopulateLedgerReviewState(rows []resellerdomain.LedgerEntry, now time.Time) error {
	for i := range rows {
		row := &rows[i]
		row.CanConfirm = false
		row.ReviewState = "not_required"
		if row.Type != resellerdomain.LedgerTypeOrderProfit && row.Type != resellerdomain.LedgerTypeRefundDeduct {
			continue
		}
		if row.Status == resellerdomain.LedgerStatusCanceled {
			row.ReviewState = "canceled"
			continue
		}
		if row.ConfirmedAt != nil || row.Status == resellerdomain.LedgerStatusAvailable || row.Status == resellerdomain.LedgerStatusLocked || row.Status == resellerdomain.LedgerStatusWithdrawn {
			row.ReviewState = "confirmed"
			continue
		}
		if row.Status != resellerdomain.LedgerStatusPendingConfirm {
			row.ReviewState = "blocked"
			continue
		}
		row.ReviewState = "blocked"
		if row.OrderID == nil {
			continue
		}
		var order orderdomain.Order
		if err := s.db.Where("id = ? AND deleted_at IS NULL", *row.OrderID).First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		if order.ResellerID == nil || *order.ResellerID != row.ResellerID || order.PaidAt == nil || order.Status == constants.OrderStatusCanceled || order.Status == constants.OrderStatusPendingPayment {
			continue
		}
		profile, err := s.GetProfileByID(row.ResellerID)
		if err != nil {
			return err
		}
		if profile == nil || profile.Status != resellerdomain.ProfileStatusActive || (profile.SettlementStatus != "" && profile.SettlementStatus != resellerdomain.SettlementStatusNormal) {
			continue
		}
		var account resellerdomain.BalanceAccount
		if err := s.db.Where("reseller_id = ? AND currency = ? AND deleted_at IS NULL", row.ResellerID, row.Currency).First(&account).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if account.Status == resellerdomain.BalanceStatusFrozenReview || account.Status == resellerdomain.BalanceStatusDisabled {
			continue
		}
		completed, err := s.GetOrderDeliveryCompletedAt(order.ID)
		if err != nil {
			return err
		}
		if completed == nil {
			row.AvailableAt = nil
			row.ReviewState = "awaiting_delivery"
			continue
		}
		if row.DeliveryCompletedAt != nil && row.DeliveryCompletedAt.After(*completed) {
			completed = row.DeliveryCompletedAt
		}
		at := completed.Add(resellerdomain.SettlementHold)
		row.DeliveryCompletedAt, row.AvailableAt = completed, &at
		eligible, err := s.IsOrderAutomaticSettlementEligible(order.ID)
		if err != nil {
			return err
		}
		if !eligible {
			row.ReviewState = "blocked"
			continue
		}
		row.ReviewState = "holding"
		if !now.Before(at) {
			row.ReviewState = "ready"
		}
	}
	return nil
}
