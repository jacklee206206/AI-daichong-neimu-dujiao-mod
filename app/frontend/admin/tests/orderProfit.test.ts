// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { itemProfitSnapshot, orderProfitSnapshot } from '../src/utils/orderProfit.ts'

describe('order owner gross profit snapshots', () => {
  it('retains only the owner margin on a reseller sale', () => {
    const item = { quantity: 1, total_price: '139.00', coupon_discount_amount: '0', cost_price: '110.00' }
    const order = { items: [item], reseller_profit_amount: '20.00' }
    assert.equal(itemProfitSnapshot(order, item).profit, 9)
    assert.equal(orderProfitSnapshot(order).profit, 9)
  })

  it('uses coupon-adjusted main-site item revenue and does not deduct earlier discounts twice', () => {
    const item = { quantity: 2, total_price: '258.00', coupon_discount_amount: '10.00', cost_price: '110.00', promotion_discount_amount: 20 }
    const order = { items: [item], reseller_profit_amount: 0, total_amount: '255.00' }
    assert.equal(orderProfitSnapshot(order).revenue, 248)
    assert.equal(orderProfitSnapshot(order).profit, 28)
  })

  it('aggregates split orders without counting parent commission or duplicate items twice', () => {
    const item = { quantity: 1, total_price: 139, cost_price: 110 }
    const child = { items: [item], reseller_profit_amount: 20 }
    const parent = { items: [item], children: [child, child], reseller_profit_amount: 40 }
    assert.deepEqual(orderProfitSnapshot(parent), { revenue: 278, cost: 220, resellerCommission: 40, profit: 18, itemCount: 2 })
  })

  it('preserves every commission cent when several items share one snapshot', () => {
    const items = [1, 1, 1].map((total_price) => ({ quantity: 1, total_price, cost_price: 0 }))
    const order = { items, reseller_profit_amount: '0.10' }
    assert.deepEqual(items.map((item) => itemProfitSnapshot(order, item).resellerCommission), [0.03, 0.03, 0.04])
    assert.equal(orderProfitSnapshot(order).profit, 2.9)
    assert.equal(itemProfitSnapshot(order, items[0]!).commissionAllocated, true)
  })

  it('does not invent refund cost recovery or reduce the original margin using refunded_amount', () => {
    const item = { quantity: 1, total_price: 139, cost_price: 110 }
    const order = { items: [item], reseller_profit_amount: 20, refunded_amount: 139, payments: [{ fee_amount: 3 }] }
    assert.equal(orderProfitSnapshot(order).profit, 9)
    assert.equal(itemProfitSnapshot(order, item).profit, 9)
  })
})
