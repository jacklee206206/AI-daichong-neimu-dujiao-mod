// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/shopspring/decimal"
)

const ManualSettlementHold = resellerdomain.SettlementHold

// RefreshPendingDeliveryEligibility records the actual delivery-based deadline.
// It never releases money. The source is real fulfillment, not order UpdatedAt.
func (s *AccountingLedgerService) RefreshPendingDeliveryEligibility() error {
	rows, err := s.store.ListPendingProfitEntries()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.OrderID == nil {
			continue
		}
		err := s.store.WithinLedgerTransaction(func(store resellercontract.AccountingLedgerStore) error {
			order, err := store.GetSettlementOrderForUpdate(*row.OrderID)
			if err != nil {
				return err
			}
			if order == nil || order.PaidAt == nil {
				return nil
			}
			if _, err := store.GetOrCreateBalanceAccountForUpdate(row.ResellerID, row.Currency); err != nil {
				return err
			}
			return refreshOrderDeliveryEligibility(store, *row.OrderID)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func refreshOrderDeliveryEligibility(store resellercontract.AccountingLedgerStore, orderID uint) error {
	deliveredAt, err := store.GetOrderDeliveryCompletedAt(orderID)
	if err != nil {
		return err
	}
	entries, err := store.ListOrderLedgerEntriesForUpdate(orderID)
	if err != nil {
		return err
	}
	// A missing/revoked fulfillment clears eligibility. A changed date cannot shorten an existing hold.
	for i := range entries {
		row := &entries[i]
		if row.Status != resellerdomain.LedgerStatusPendingConfirm {
			continue
		}
		if row.Type != resellerdomain.LedgerTypeOrderProfit && row.Type != resellerdomain.LedgerTypeRefundDeduct {
			continue
		}
		if deliveredAt == nil {
			if row.AvailableAt == nil {
				continue
			}
			row.AvailableAt = nil
		} else {
			anchor := *deliveredAt
			if row.DeliveryCompletedAt != nil && row.DeliveryCompletedAt.After(anchor) {
				anchor = *row.DeliveryCompletedAt
			}
			eligible := anchor.Add(ManualSettlementHold)
			if row.AvailableAt != nil && row.AvailableAt.Equal(eligible) && row.DeliveryCompletedAt != nil && row.DeliveryCompletedAt.Equal(anchor) {
				continue
			}
			row.DeliveryCompletedAt = &anchor
			row.AvailableAt = &eligible
		}
		if err := store.UpdateLedgerEntry(row); err != nil {
			return err
		}
	}
	return nil
}

// ConfirmOrderProfit atomically releases one order's profit together with every pending refund debit.
// Repeating a successful review is harmless, even if money has subsequently been locked/withdrawn.
func (s *AccountingLedgerService) ConfirmOrderProfit(adminID, entryID uint, reason string) (*resellerdomain.LedgerEntry, error) {
	return s.confirmOrderProfitAt(adminID, entryID, reason, time.Now())
}

func (s *AccountingLedgerService) confirmOrderProfitAt(adminID, entryID uint, reason string, now time.Time) (*resellerdomain.LedgerEntry, error) {
	if s == nil || s.store == nil || adminID == 0 || entryID == 0 {
		return nil, resellercontract.ErrLedgerReviewInvalid
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 1000 {
		return nil, resellercontract.ErrLedgerReviewReasonRequired
	}
	err := s.store.WithinLedgerTransaction(func(store resellercontract.AccountingLedgerStore) error {
		entry, err := store.GetLedgerEntryByID(entryID)
		if err != nil {
			return err
		}
		if entry == nil || entry.Type != resellerdomain.LedgerTypeOrderProfit || entry.OrderID == nil {
			return resellercontract.ErrLedgerReviewInvalid
		}
		order, err := store.GetSettlementOrderForUpdate(*entry.OrderID)
		if err != nil {
			return err
		}
		if order == nil || order.ResellerID == nil || *order.ResellerID != entry.ResellerID {
			return resellercontract.ErrLedgerReviewInvalid
		}
		balance, err := store.GetOrCreateBalanceAccountForUpdate(entry.ResellerID, entry.Currency)
		if err != nil {
			return err
		}
		// Re-read after locks so a concurrent review cannot double-release.
		entry, err = store.GetLedgerEntryByID(entryID)
		if err != nil {
			return err
		}
		if entry.ConfirmedAt != nil {
			return nil
		}
		if entry.Status != resellerdomain.LedgerStatusPendingConfirm {
			return resellercontract.ErrLedgerReviewInvalid
		}
		profile, err := store.GetProfileByID(entry.ResellerID)
		if err != nil {
			return err
		}
		if err := RequireActiveProfile(profile); err != nil {
			return err
		}
		if balance.Status == resellerdomain.BalanceStatusFrozenReview || balance.Status == resellerdomain.BalanceStatusDisabled {
			return resellercontract.ErrBalanceAccountFrozen
		}
		if order.PaidAt == nil || order.Status == constants.OrderStatusCanceled || order.Status == constants.OrderStatusPendingPayment {
			return resellercontract.ErrLedgerNotReady
		}
		if err := refreshOrderDeliveryEligibility(store, order.ID); err != nil {
			return err
		}
		entry, err = store.GetLedgerEntryByID(entryID)
		if err != nil {
			return err
		}
		if entry.AvailableAt == nil || entry.DeliveryCompletedAt == nil || now.Before(*entry.AvailableAt) {
			return resellercontract.ErrLedgerNotReady
		}
		entries, err := store.ListOrderLedgerEntriesForUpdate(order.ID)
		if err != nil {
			return err
		}
		net := decimal.Zero
		selected := make([]*resellerdomain.LedgerEntry, 0)
		for i := range entries {
			row := &entries[i]
			if row.Type != resellerdomain.LedgerTypeOrderProfit && row.Type != resellerdomain.LedgerTypeRefundDeduct {
				continue
			}
			if row.ResellerID != entry.ResellerID || row.Currency != entry.Currency || row.Status != resellerdomain.LedgerStatusPendingConfirm {
				return resellercontract.ErrLedgerReviewInvalid
			}
			net = net.Add(row.Amount.Decimal)
			selected = append(selected, row)
		}
		if net.IsNegative() {
			return resellercontract.ErrLedgerReviewInvalid
		}
		for _, row := range selected {
			row.Status = resellerdomain.LedgerStatusAvailable
			if net.IsZero() {
				row.Status = resellerdomain.LedgerStatusCanceled
			}
			row.ConfirmationMode = resellerdomain.ConfirmationModeManual
			row.ConfirmedBy = &adminID
			row.ConfirmedAt = &now
			row.ConfirmationReason = reason
			if err := store.UpdateLedgerEntry(row); err != nil {
				return err
			}
		}
		return RefreshBalanceAccount(store, entry.ResellerID, entry.Currency, now)
	})
	if err != nil {
		return nil, err
	}
	return s.store.GetLedgerEntryByID(entryID)
}

// confirmAutomaticOrderProfitAt serializes on order -> balance, just like the
// refund path. It only releases the order's positive profit, never a raw due row.
func (s *AccountingLedgerService) confirmAutomaticOrderProfitAt(entryID uint, now time.Time) (bool, error) {
	released := false
	err := s.store.WithinLedgerTransaction(func(store resellercontract.AccountingLedgerStore) error {
		entry, err := store.GetLedgerEntryByID(entryID)
		if err != nil {
			return err
		}
		if entry == nil || entry.OrderID == nil || entry.Type != resellerdomain.LedgerTypeOrderProfit {
			return nil
		}
		order, err := store.GetSettlementOrderForUpdate(*entry.OrderID)
		if err != nil {
			return err
		}
		if order == nil || order.ResellerID == nil || *order.ResellerID != entry.ResellerID || order.Currency != entry.Currency || order.PaidAt == nil {
			return nil
		}
		balance, err := store.GetOrCreateBalanceAccountForUpdate(entry.ResellerID, entry.Currency)
		if err != nil {
			return err
		}
		entry, err = store.GetLedgerEntryByID(entryID)
		if err != nil {
			return err
		}
		if entry == nil || entry.Status != resellerdomain.LedgerStatusPendingConfirm || entry.ConfirmedAt != nil || !entry.Amount.Decimal.IsPositive() {
			return nil
		}
		// Recompute even historical manual/payment-based deadlines. A later
		// delivery cannot shorten an already recorded delivery anchor.
		if err := refreshOrderDeliveryEligibility(store, order.ID); err != nil {
			return err
		}
		entry, err = store.GetLedgerEntryByID(entryID)
		if err != nil {
			return err
		}
		if entry.AvailableAt == nil || entry.DeliveryCompletedAt == nil || now.Before(*entry.AvailableAt) {
			return nil
		}
		profile, err := store.GetProfileByID(entry.ResellerID)
		if err != nil {
			return err
		}
		if RequireActiveProfile(profile) != nil || balance.Status == resellerdomain.BalanceStatusFrozenReview || balance.Status == resellerdomain.BalanceStatusDisabled {
			return nil
		}
		eligible, err := store.IsOrderAutomaticSettlementEligible(order.ID)
		if err != nil || !eligible {
			return err
		}
		entries, err := store.ListOrderLedgerEntriesForUpdate(order.ID)
		if err != nil {
			return err
		}
		for _, row := range entries {
			// Any refund debit or unexpected second credit is an exception for
			// review, even if the order status was subsequently edited.
			if row.Type == resellerdomain.LedgerTypeRefundDeduct ||
				(row.Type == resellerdomain.LedgerTypeOrderProfit && row.ID != entry.ID) {
				return nil
			}
		}
		entry.Status = resellerdomain.LedgerStatusAvailable
		entry.ConfirmationMode = resellerdomain.ConfirmationModeAuto
		entry.ConfirmedBy = nil
		entry.ConfirmedAt = &now
		entry.ConfirmationReason = "订单实际交付满 92 小时，无异常或退款，系统自动确认"
		if err := store.UpdateLedgerEntry(entry); err != nil {
			return err
		}
		if err := RefreshBalanceAccount(store, entry.ResellerID, entry.Currency, now); err != nil {
			return err
		}
		released = true
		return nil
	})
	return released && err == nil, err
}
