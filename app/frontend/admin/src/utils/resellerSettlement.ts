// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

type ReviewableProfit = {
  type?: string
  status?: string
  confirmation_mode?: string
  review_state?: string
  can_confirm?: boolean
}

// Eligibility comes from the server. A local clock or a passed date cannot release money.
export const canReviewResellerProfit = (entry: ReviewableProfit, hasPermission: boolean) =>
  hasPermission && entry.type === 'order_profit' && entry.status === 'pending_confirm' &&
  entry.confirmation_mode === 'manual' && entry.review_state === 'ready' && entry.can_confirm === true

export const resellerReviewStateKey = (state?: string) =>
  ['awaiting_delivery', 'holding', 'ready', 'confirmed', 'blocked', 'legacy_auto', 'canceled', 'not_required'].includes(state || '') ? state : 'unknown'
