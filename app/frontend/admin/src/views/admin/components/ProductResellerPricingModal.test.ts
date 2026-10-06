// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import ProductResellerPricingModal from './ProductResellerPricingModal.vue'

const api = vi.hoisted(() => ({ getProduct: vi.fn(), updateProductResellerPrices: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/notify', () => ({ notifySuccess: vi.fn() }))
vi.mock('@/utils/format', () => ({ getLocalizedText: (value: Record<string, string>) => value['zh-CN'], formatMoney: (value: string | number) => Number(value).toFixed(2) }))
vi.mock('@/components/ui/dialog', async () => {
  const { defineComponent, h } = await import('vue')
  const container = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })
  return { Dialog: container, DialogScrollContent: container, DialogHeader: container, DialogTitle: container }
})

let app: App | undefined
let host: HTMLDivElement
const settled = async () => { await Promise.resolve(); await nextTick(); await Promise.resolve(); await nextTick() }
const load = async () => {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(ProductResellerPricingModal, { modelValue: true, productId: 5, siteCurrency: 'CNY' })
  app.mount(host)
  await settled()
}
const input = async (id: string, value: string) => {
  const element = host.querySelector<HTMLInputElement>(`#${id}`)!
  element.value = value
  element.dispatchEvent(new Event('input', { bubbles: true }))
  await settled()
}
const saveButton = () => [...host.querySelectorAll('button')].find(button => button.textContent?.includes('resellerPricing.save'))!

beforeEach(() => {
  api.getProduct.mockResolvedValue({ data: { data: { id: 5, title: { 'zh-CN': 'Test product' }, skus: [{ id: 9, sku_code: 'month', spec_values: {}, price_amount: 124, reseller_supply_price_amount: 118, reseller_min_price_amount: 125 }] } } })
  api.updateProductResellerPrices.mockResolvedValue({ data: {} })
})
afterEach(() => { app?.unmount(); host?.remove(); vi.clearAllMocks() })

describe('admin reseller pricing dialog', () => {
  it('sends only SKU supply and floor values after editing numeric inputs', async () => {
    await load()
    await input('reseller-supply-9', '118.50')
    await input('reseller-minimum-9', '126')
    saveButton().click()
    await settled()
    expect(api.updateProductResellerPrices).toHaveBeenCalledWith(5, { skus: [{ id: 9, reseller_supply_price_amount: '118.50', reseller_min_price_amount: '126.00' }] })
  })
  it('blocks empty, negative and over-precision amounts, but accepts explicit zero inheritance', async () => {
    await load()
    for (const invalid of ['', '-1', '125.001']) {
      await input('reseller-minimum-9', invalid)
      expect(saveButton().disabled).toBe(true)
    }
    expect(api.updateProductResellerPrices).not.toHaveBeenCalled()
    await input('reseller-minimum-9', '0')
    expect(saveButton().disabled).toBe(false)
    saveButton().click()
    await settled()
    expect(api.updateProductResellerPrices.mock.calls[0]?.[1].skus[0].reseller_min_price_amount).toBe('0.00')
  })
  it('keeps edited values available for retry after the server rejects a save', async () => {
    api.updateProductResellerPrices.mockRejectedValueOnce(new Error('SKU changed; refresh and retry'))
    await load()
    await input('reseller-minimum-9', '130')
    saveButton().click()
    await settled()
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('SKU changed')
    expect(host.querySelector<HTMLInputElement>('#reseller-minimum-9')?.value).toBe('130')
    expect(saveButton().disabled).toBe(false)
  })
})
