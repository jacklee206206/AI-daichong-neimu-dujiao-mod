// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

type Amount = string | number | undefined

export interface ProfitItem {
  quantity: number
  total_price: Amount
  coupon_discount_amount?: Amount
  cost_price: Amount
}

export interface ProfitOrder {
  items?: ProfitItem[]
  children?: ProfitOrder[]
  reseller_profit_amount?: Amount
}

const cents = (value: Amount) => {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? Math.round(parsed * 100) : 0
}

const revenueCents = (item: ProfitItem) => cents(item.total_price) - cents(item.coupon_discount_amount)
const costCents = (item: ProfitItem) => cents(item.cost_price) * item.quantity

// This is the sale snapshot, before refunds and payment fees. A refund amount alone
// cannot tell us whether supplier cost, reseller commission or fees were recovered.
export function orderProfitSnapshot(order: ProfitOrder | null): {
  revenue: number
  cost: number
  resellerCommission: number
  profit: number
  itemCount: number
} {
  if (!order) return { revenue: 0, cost: 0, resellerCommission: 0, profit: 0, itemCount: 0 }
  const children = order.children || []
  const items = order.items || []
  const childSnapshots = children.map(orderProfitSnapshot)
  const revenue = children.length
    ? childSnapshots.reduce((sum, child) => sum + cents(child.revenue), 0)
    : items.reduce((sum, item) => sum + revenueCents(item), 0)
  const cost = children.length
    ? childSnapshots.reduce((sum, child) => sum + cents(child.cost), 0)
    : items.reduce((sum, item) => sum + costCents(item), 0)
  // Parent commission is already the aggregate: do not subtract child commission again.
  const commission = order.reseller_profit_amount !== undefined
    ? cents(order.reseller_profit_amount)
    : childSnapshots.reduce((sum, child) => sum + cents(child.resellerCommission), 0)
  return {
    revenue: revenue / 100,
    cost: cost / 100,
    resellerCommission: commission / 100,
    profit: (revenue - cost - commission) / 100,
    itemCount: children.length ? childSnapshots.reduce((sum, child) => sum + child.itemCount, 0) : items.length,
  }
}

export function itemProfitSnapshot(order: ProfitOrder | null, item: ProfitItem) {
  const items = order?.items || []
  const index = items.indexOf(item)
  const totalCommission = cents(order?.reseller_profit_amount)
  const weights = items.map((row) => Math.max(revenueCents(row), 0))
  const totalWeight = weights.reduce((sum, weight) => sum + weight, 0)
  const shares = weights.map((weight) => totalWeight > 0
    ? Math.floor(totalCommission * weight / totalWeight)
    : Math.floor(totalCommission / items.length))
  // Allocate the remainder once so item rows add up to the order's stored commission.
  const assigned = shares.reduce((sum, share) => sum + share, 0)
  if (shares.length) shares[shares.length - 1] = (shares[shares.length - 1] || 0) + totalCommission - assigned
  const commission = index >= 0 ? shares[index] || 0 : 0
  return {
    resellerCommission: commission / 100,
    profit: (revenueCents(item) - costCents(item) - commission) / 100,
    commissionAllocated: items.length > 1 && totalCommission > 0,
  }
}
