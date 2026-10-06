// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

package domain

const (
	ProfileStatusPendingReview = "pending_review"
	ProfileStatusActive        = "active"
	ProfileStatusRejected      = "rejected"
	ProfileStatusDisabled      = "disabled"

	SettlementStatusNormal = "normal"
	SettlementStatusFrozen = "frozen"

	DomainTypeSubdomain = "subdomain"
	DomainTypeCustom    = "custom"

	DomainVerificationPending  = "pending"
	DomainVerificationVerified = "verified"
	DomainVerificationFailed   = "failed"

	DomainStatusPendingReview = "pending_review"
	DomainStatusActive        = "active"
	DomainStatusDisabled      = "disabled"

	PricingModeInherit       = "inherit"
	PricingModeMarkupPercent = "markup_percent"
	PricingModeFixedMarkup   = "fixed_markup"
	PricingModeFixedPrice    = "fixed_price"

	LedgerTypeOrderProfit  = "order_profit"
	LedgerTypeRefundDeduct = "refund_deduct"
	LedgerTypeManualAdjust = "manual_adjust"
	LedgerTypeWithdrawLock = "withdraw_lock"
	LedgerTypeWithdrawPaid = "withdraw_paid"

	ConfirmationModeManual = "manual"
	ConfirmationModeAuto   = "auto"

	LedgerStatusPendingConfirm = "pending_confirm"
	LedgerStatusAvailable      = "available"
	LedgerStatusLocked         = "locked"
	LedgerStatusWithdrawn      = "withdrawn"
	LedgerStatusCanceled       = "canceled"

	WithdrawStatusPending  = "pending"
	WithdrawStatusRejected = "rejected"
	WithdrawStatusPaid     = "paid"

	BalanceStatusNormal          = "normal"
	BalanceStatusNegativeBalance = "negative_balance"
	BalanceStatusFrozenReview    = "frozen_review"
	BalanceStatusDisabled        = "disabled"

	RelatedAccountStatusActive   = "active"
	RelatedAccountStatusDisabled = "disabled"
)
