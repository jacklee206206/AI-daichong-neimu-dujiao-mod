// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package application

import (
	"fmt"
	"strings"
	"time"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

// AccountingLedgerService 分销利润入账、退款扣减与到期确认用例。
type AccountingLedgerService struct {
	store            resellercontract.AccountingLedgerStore
	confirmationMode string
}

// The old day count is accepted for source compatibility; settlement is anchored
// to real delivery and the shared 92-hour policy, never the payment timestamp.
func NewAccountingLedgerService(store resellercontract.AccountingLedgerStore, _ int, modes ...string) *AccountingLedgerService {
	mode := resellerdomain.ConfirmationModeAuto
	if len(modes) > 0 && strings.TrimSpace(modes[0]) == resellerdomain.ConfirmationModeManual {
		mode = resellerdomain.ConfirmationModeManual
	}
	return &AccountingLedgerService{store: store, confirmationMode: mode}
}

// PostOrderProfit 在调用方已开启的事务 store 上写入订单利润流水。
func (s *AccountingLedgerService) PostOrderProfit(store resellercontract.AccountingLedgerStore, order *orderdomain.Order, payment *paymentdomain.Payment) error {
	if s == nil || store == nil || order == nil || order.ID == 0 {
		return nil
	}
	if order.ResellerID == nil || *order.ResellerID == 0 {
		return nil
	}
	snapshot, err := store.GetOrderSnapshotByOrderID(order.ID)
	if err != nil {
		return err
	}
	if snapshot == nil {
		logger.Warnw("reseller_accounting_missing_snapshot_skip", "order_id", order.ID, "order_no", order.OrderNo)
		return nil
	}
	if !snapshot.ProfitEligible {
		return nil
	}
	profit := snapshot.ProfitAmount.Decimal.Round(2)
	if profit.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	now := time.Now()
	orderID := order.ID
	metadata := jsonmap.JSON{
		"order_no":            order.OrderNo,
		"reseller_domain":     snapshot.Domain,
		"wallet_paid_amount":  order.WalletPaidAmount.String(),
		"online_paid_amount":  order.OnlinePaidAmount.String(),
		"snapshot_id":         snapshot.ID,
		"profit_block_reason": snapshot.ProfitBlockReason,
	}
	if payment != nil {
		metadata["payment_id"] = payment.ID
		metadata["payment_channel_id"] = payment.ChannelID
		metadata["payment_amount"] = payment.Amount.String()
		metadata["payment_status"] = payment.Status
	}
	entry := &resellerdomain.LedgerEntry{
		ResellerID:       snapshot.ResellerID,
		OrderID:          &orderID,
		Type:             resellerdomain.LedgerTypeOrderProfit,
		Amount:           money.FromDecimal(profit),
		Currency:         strings.TrimSpace(snapshot.Currency),
		IdempotencyKey:   fmt.Sprintf("order_profit:%d", order.ID),
		MetadataJSON:     metadata,
		Status:           resellerdomain.LedgerStatusPendingConfirm,
		ConfirmationMode: s.confirmationMode,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if entry.Currency == "" {
		entry.Currency = strings.TrimSpace(order.Currency)
	}
	if entry.Currency == "" {
		return resellercontract.ErrLedgerInvalidSnapshot
	}
	if _, err := store.GetOrCreateBalanceAccountForUpdate(snapshot.ResellerID, entry.Currency); err != nil {
		return err
	}
	created, err := store.CreateLedgerEntryIfNotExists(entry)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	return RefreshBalanceAccount(store, snapshot.ResellerID, entry.Currency, now)
}

// PostOrderProfitForOrder 在订单状态流转事务中入账分销利润。
// 订单用例不需要感知尚未迁移的支付持久化实体。
func (s *AccountingLedgerService) PostOrderProfitForOrder(store resellercontract.AccountingLedgerStore, order *orderdomain.Order) error {
	return s.PostOrderProfit(store, order, nil)
}

// ConfirmDueLedgerEntries checks each order under the same locks used by refunds
// and withdrawals. Historical manual pending entries use this policy as well.
func (s *AccountingLedgerService) ConfirmDueLedgerEntries(now time.Time) (int64, error) {
	if s == nil || s.store == nil {
		return 0, nil
	}
	if s.confirmationMode != resellerdomain.ConfirmationModeAuto {
		return 0, s.RefreshPendingDeliveryEligibility()
	}
	rows, err := s.store.ListPendingProfitEntries()
	if err != nil {
		return 0, err
	}
	var affected int64
	for _, row := range rows {
		released, err := s.confirmAutomaticOrderProfitAt(row.ID, now)
		if err != nil {
			return affected, err
		}
		if released {
			affected++
		}
	}
	return affected, nil
}
