// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import test from 'node:test'
import assert from 'node:assert/strict'
import {
  getResellerFinanceStatusView,
  getResellerBalanceWithdrawDisabledReason,
  getResellerMinimumWithdrawBalanceCNY,
  getResellerLedgerTypeKey,
  getResellerWithdrawDisabledReasonKey,
  isResellerWithdrawEnabled,
} from '../src/utils/resellerFinance.ts'

test('CNY wallet eligibility starts at 50 and does not impose a withdrawal amount minimum', () => {
  const account = { currency: 'CNY', status: 'normal', negative_amount: '0.00', available_amount: '49.99' }
  assert.equal(getResellerBalanceWithdrawDisabledReason(account), 'belowMinimum')
  assert.equal(getResellerBalanceWithdrawDisabledReason({ ...account, available_amount: '50.00' }), null)
  assert.equal(getResellerBalanceWithdrawDisabledReason({ ...account, available_amount: '100.00' }), null)
  // Threshold applies to CNY only, with no client-side currency conversion.
  assert.equal(getResellerBalanceWithdrawDisabledReason({ ...account, currency: 'USDT', available_amount: '7.00' }), null)
})

test('reseller wallet guard excludes missing, invalid, frozen, or indebted balances', () => {
  assert.equal(getResellerBalanceWithdrawDisabledReason(null), 'noBalance')
  const account = { currency: 'CNY', status: 'normal', available_amount: '100.00' }
  assert.equal(getResellerBalanceWithdrawDisabledReason({ ...account, available_amount: 'NaN' }), 'noBalance')
  for (const status of ['negative_balance', 'frozen_review', 'disabled']) {
    assert.equal(getResellerBalanceWithdrawDisabledReason({ ...account, status }), 'balanceFrozen')
  }
  assert.equal(getResellerBalanceWithdrawDisabledReason({ ...account, negative_amount: '0.01' }), 'balanceFrozen')
})

test('reseller withdrawal threshold reads server policy and falls back to CNY 50', () => {
  assert.equal(getResellerMinimumWithdrawBalanceCNY('75.00'), 75)
  for (const value of [undefined, '', 'NaN', '0', '-1']) assert.equal(getResellerMinimumWithdrawBalanceCNY(value), 50)
  const account = { currency: 'CNY', available_amount: '60.00' }
  assert.equal(getResellerBalanceWithdrawDisabledReason(account, 75), 'belowMinimum')
})

test('reseller withdraw availability follows dashboard contract', () => {
  assert.equal(isResellerWithdrawEnabled({ withdraw_enabled: true }), true)
  assert.equal(isResellerWithdrawEnabled({ withdraw_enabled: false }), false)
  assert.equal(isResellerWithdrawEnabled(null), false)
})

test('reseller withdraw disabled reason maps backend reason keys', () => {
  assert.equal(getResellerWithdrawDisabledReasonKey('profile_inactive'), 'profileInactive')
  assert.equal(getResellerWithdrawDisabledReasonKey('settlement_unavailable'), 'settlementUnavailable')
  assert.equal(getResellerWithdrawDisabledReasonKey('unexpected'), 'default')
  assert.equal(getResellerWithdrawDisabledReasonKey(undefined), 'default')
})

test('reseller finance status prioritizes inactive profile before settlement status', () => {
  assert.deepEqual(
    getResellerFinanceStatusView(null),
    { namespace: 'profileStatusMap', key: 'unknown', badgeTone: 'neutral' },
  )
  assert.deepEqual(
    getResellerFinanceStatusView({ status: 'active', settlement_status: 'normal' }),
    { namespace: 'settlementStatusMap', key: 'normal', badgeTone: 'success' },
  )
  assert.deepEqual(
    getResellerFinanceStatusView({ status: 'active', settlement_status: 'frozen' }),
    { namespace: 'settlementStatusMap', key: 'frozen', badgeTone: 'warning' },
  )
  assert.deepEqual(
    getResellerFinanceStatusView({ status: 'disabled', settlement_status: 'normal' }),
    { namespace: 'profileStatusMap', key: 'disabled', badgeTone: 'neutral' },
  )
})

test('reseller ledger type mapping includes all backend ledger types', () => {
  assert.equal(getResellerLedgerTypeKey('order_profit'), 'orderProfit')
  assert.equal(getResellerLedgerTypeKey('refund_deduct'), 'refundDeduct')
  assert.equal(getResellerLedgerTypeKey('withdraw_lock'), 'withdrawLock')
  assert.equal(getResellerLedgerTypeKey('manual_adjust'), 'manualAdjust')
  assert.equal(getResellerLedgerTypeKey('withdraw_paid'), 'withdrawPaid')
  assert.equal(getResellerLedgerTypeKey('other'), null)
})
