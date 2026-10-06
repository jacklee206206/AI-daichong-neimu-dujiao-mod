// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive, type App } from 'vue'
import type { AdminOrder } from '@/api/types'
import OrderDetailDialog from './OrderDetailDialog.vue'

const api = vi.hoisted(() => ({ getOrder: vi.fn(), getProcurementOrders: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: api }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'zh-CN' } }),
}))
vi.mock('@/utils/format', () => ({
  getLocalizedText: (value: Record<string, string>) => value?.['zh-CN'] || '',
  formatDate: (value: string) => value || '',
  formatMoney: (value: string | number, currency: string) => `${Number(value).toFixed(2)} ${currency}`,
  hasPositiveAmount: (value: string | number) => Number(value) > 0,
}))
vi.mock('@/components/ui/dialog', async () => {
  const { defineComponent, h } = await import('vue')
  const container = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })
  return { Dialog: container, DialogScrollContent: container, DialogHeader: container, DialogTitle: container }
})

let app: App | undefined
let host: HTMLDivElement
const settled = async () => { for (let i = 0; i < 5; i += 1) { await Promise.resolve(); await nextTick() } }
const order = (overrides: Record<string, unknown> = {}) => ({
  id: 1, order_no: 'PARENT-1', user_id: 1, currency: 'CNY', status: 'completed',
  paid_at: '2026-10-03T00:00:00Z', created_at: '2026-10-03T00:00:00Z',
  total_amount: 139, refunded_amount: 0, reseller_profit_amount: 20,
  items: [{ id: 1, product_id: 2, quantity: 1, title: { 'zh-CN': '示例商品 A（虚构）' }, total_price: 139, cost_price: 110, fulfillment_type: 'upstream' }],
  ...overrides,
} as unknown as AdminOrder)

const load = async (data: AdminOrder) => {
  api.getOrder.mockResolvedValue({ data: { data } })
  host = document.createElement('div')
  document.body.append(host)
  const props = reactive({ modelValue: false, order: data, siteCurrency: 'CNY', maxRefundDays: 0 })
  app = createApp({ render: () => h(OrderDetailDialog, props) })
  app.mount(host)
  props.modelValue = true
  await settled()
}

beforeEach(() => {
  api.getProcurementOrders.mockResolvedValue({ data: { data: [{ id: 3, status: 'fulfilled', upstream_amount: 119, upstream_currency: 'CNY' }] } })
})
afterEach(() => { app?.unmount(); host?.remove(); vi.clearAllMocks() })

describe('admin order profit display', () => {
  it('shows owner margin on parent orders with child items, and keeps supplier debit separate', async () => {
    await load(order({ items: [], children: [order({ id: 2, parent_id: 1 })] }))
    const text = host.textContent || ''
    expect(text).toContain('admin.orders.orderProfit')
    expect(text).toContain('admin.orders.itemProfit：9.00 CNY')
    expect(text).toContain('admin.orders.resellerCommission：20.00 CNY')
    expect(text).toContain('admin.orders.upstreamActualDebit:119.00 CNY')
    expect(text).toContain('admin.orders.upstreamDebitHint')
    expect(text).not.toContain('29.00 CNY')
  })

  it('labels a refunded order as an original margin snapshot and shows customer refund separately', async () => {
    await load(order({ status: 'partially_refunded', refunded_amount: 10 }))
    const text = host.textContent || ''
    expect(text).toContain('admin.orders.itemProfit：9.00 CNY')
    expect(text).toContain('admin.orders.customerRefunded：10.00 CNY')
    expect(text).toContain('admin.orders.refundProfitHint')
    expect(text).not.toContain('admin.orders.itemRefund：')
  })
})
