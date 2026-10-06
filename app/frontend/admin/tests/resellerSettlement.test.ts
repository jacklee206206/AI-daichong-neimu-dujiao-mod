// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
const expect = (value: unknown) => ({ toBe: (expected: unknown) => assert.equal(value, expected) })
import { canReviewResellerProfit, resellerReviewStateKey } from '../src/utils/resellerSettlement.ts'

describe('manual reseller profit review', () => {
  const ready = { type: 'order_profit', status: 'pending_confirm', confirmation_mode: 'manual', review_state: 'ready', can_confirm: true }
  it('requires authoritative eligibility and permission', () => {
    expect(canReviewResellerProfit(ready, true)).toBe(true)
    expect(canReviewResellerProfit(ready, false)).toBe(false)
    expect(canReviewResellerProfit({ ...ready, can_confirm: undefined }, true)).toBe(false)
    expect(canReviewResellerProfit({ ...ready, review_state: 'holding' }, true)).toBe(false)
    expect(canReviewResellerProfit({ ...ready, confirmation_mode: 'auto' }, true)).toBe(false)
    expect(canReviewResellerProfit({ ...ready, status: 'available' }, true)).toBe(false)
    expect(canReviewResellerProfit({ ...ready, type: 'refund_deduct' }, true)).toBe(false)
  })
  it('keeps missing or future review states unknown', () => {
    expect(resellerReviewStateKey(undefined)).toBe('unknown')
    expect(resellerReviewStateKey('new_state')).toBe('unknown')
    expect(resellerReviewStateKey('awaiting_delivery')).toBe('awaiting_delivery')
  })
})
