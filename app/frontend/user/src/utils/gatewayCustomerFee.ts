// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { amountToCents, calculateFeeCents, rateToBasisPoints } from './money.ts'

export interface GatewayFeeFields {
  gateway_customer_fee_rate?: unknown
  gateway_customer_fixed_fee?: unknown
  gateway_customer_fee_amount?: unknown
  gateway_customer_payable_amount?: unknown
}

export const hasGatewayCustomerFee = (channel?: GatewayFeeFields | null): boolean =>
  (rateToBasisPoints(channel?.gateway_customer_fee_rate) || 0) > 0 ||
  (amountToCents(channel?.gateway_customer_fixed_fee) || 0) > 0

// This is a disclosure estimate only. Never send these totals as the gateway order amount.
export const estimateGatewayCustomerFee = (
  channel: (GatewayFeeFields & { provider_type?: unknown; fee_policy?: unknown; fee_rate?: unknown; fixed_fee?: unknown }) | null | undefined,
  orderAmount: unknown,
  walletAmount: unknown = '0',
) => {
  const orderCents = amountToCents(orderAmount)
  if (orderCents === null || orderCents < 0 || (!hasGatewayCustomerFee(channel) && channel?.provider_type !== 'dujiaopay')) return null
  const walletCents = Math.min(orderCents, Math.max(0, amountToCents(walletAmount) || 0))
  const onlineCents = orderCents - walletCents
  if (onlineCents <= 0) return null
  const shopFeeCents = channel?.fee_policy === 'customer_surcharge'
    ? (calculateFeeCents(onlineCents, Math.max(0, rateToBasisPoints(channel.fee_rate) || 0)) || 0) + Math.max(0, amountToCents(channel.fixed_fee) || 0)
    : 0
  const requestCents = onlineCents + shopFeeCents
  const gatewayFeeCents = (calculateFeeCents(requestCents, Math.max(0, rateToBasisPoints(channel?.gateway_customer_fee_rate) || 0)) || 0) + Math.max(0, amountToCents(channel?.gateway_customer_fixed_fee) || 0)
  const onlinePayableCents = requestCents + gatewayFeeCents
  return { orderCents, walletCents, shopFeeCents, gatewayFeeCents, onlinePayableCents, totalCents: walletCents + onlinePayableCents }
}

// Do not reconstruct an old payment using the channel's current rate.
export const gatewayCustomerFeeSnapshot = (payment?: GatewayFeeFields | null) => {
  const feeCents = amountToCents(payment?.gateway_customer_fee_amount)
  const payableCents = amountToCents(payment?.gateway_customer_payable_amount)
  if (feeCents === null || payableCents === null || feeCents < 0 || payableCents < feeCents) return null
  return { feeCents, payableCents }
}
