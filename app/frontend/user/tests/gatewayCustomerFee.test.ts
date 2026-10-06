// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import test from 'node:test'
import assert from 'node:assert/strict'
import { estimateGatewayCustomerFee, gatewayCustomerFeeSnapshot } from '../src/utils/gatewayCustomerFee.ts'
import { shouldAutoOpenPaymentLink } from '../src/utils/paymentResumePolicy.ts'

const channel = { gateway_customer_fee_rate: '3.20', gateway_customer_fixed_fee: '0', fee_rate: '0', fixed_fee: '0', fee_policy: 'customer_surcharge' }
test('provider charge is disclosed once with cent rounding', () => {
  assert.deepEqual(estimateGatewayCustomerFee(channel, '124'), { orderCents: 12400, walletCents: 0, shopFeeCents: 0, gatewayFeeCents: 397, onlinePayableCents: 12797, totalCents: 12797 })
  assert.equal(estimateGatewayCustomerFee(channel, '125')?.totalCents, 12900)
  assert.equal(estimateGatewayCustomerFee(channel, '99')?.totalCents, 10217)
  assert.equal(channel.fee_rate, '0')
})
test('only the online part is charged and no fee for full wallet payment', () => {
  assert.deepEqual(estimateGatewayCustomerFee(channel, '124', '100'), { orderCents: 12400, walletCents: 10000, shopFeeCents: 0, gatewayFeeCents: 77, onlinePayableCents: 2477, totalCents: 12477 })
  assert.equal(estimateGatewayCustomerFee(channel, '124', '124'), null)
  assert.equal(estimateGatewayCustomerFee({}, '124'), null)
})
test('local surcharge and provider fixed fee are both accounted for if configured', () => {
  assert.equal(estimateGatewayCustomerFee({ ...channel, fee_rate: '2', fixed_fee: '1', gateway_customer_fixed_fee: '1' }, '100')?.totalCents, 10730)
})
test('switching to USDT clears external fee and restores goods total', () => {
  const crypto = { provider_type: 'dujiaopay', fee_rate: '0', fixed_fee: '0', fee_policy: 'customer_surcharge' }
  assert.equal(estimateGatewayCustomerFee(channel, '125')?.totalCents, 12900)
  assert.deepEqual(estimateGatewayCustomerFee(crypto, '125'), { orderCents: 12500, walletCents: 0, shopFeeCents: 0, gatewayFeeCents: 0, onlinePayableCents: 12500, totalCents: 12500 })
  assert.equal(estimateGatewayCustomerFee(crypto, '125', '100')?.totalCents, 12500)
  assert.equal(estimateGatewayCustomerFee({ ...channel, gateway_customer_fee_rate: '1' }, '125')?.totalCents, 12625)
})
test('old payments are never recalculated using a current channel rate', () => {
  assert.equal(gatewayCustomerFeeSnapshot({}), null)
  assert.equal(gatewayCustomerFeeSnapshot({ gateway_customer_fee_amount: null, gateway_customer_payable_amount: null }), null)
  assert.deepEqual(gatewayCustomerFeeSnapshot({ gateway_customer_fee_amount: '3.97', gateway_customer_payable_amount: '127.97' }), { feeCents: 397, payableCents: 12797 })
  assert.equal(shouldAutoOpenPaymentLink({ provider_type: 'epay', interaction_mode: 'redirect', pay_url: 'https://pay.example/submit', gateway_customer_fee_amount: '3.97' }), false)
})
