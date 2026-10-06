// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive, type App } from 'vue'
import ProductApiPricingModal from './ProductApiPricingModal.vue'

const api = vi.hoisted(() => ({ getProduct: vi.fn(), updateProductApiPrices: vi.fn() }))
const auth = vi.hoisted(() => ({ hasPermission: vi.fn() }))
const notifySuccess = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin', () => ({ adminAPI: api }))
vi.mock('@/stores/auth', () => ({ useAdminAuthStore: () => auth }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/notify', () => ({ notifySuccess }))
vi.mock('@/utils/format', () => ({ getLocalizedText: (value: Record<string, string>) => value['zh-CN'], formatMoney: (value: string | number) => Number(value).toFixed(2) }))
vi.mock('@/components/ui/dialog', async () => {
  const { defineComponent, h } = await import('vue')
  const container = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })
  return { Dialog: container, DialogScrollContent: container, DialogHeader: container, DialogTitle: container, DialogDescription: container }
})

let app: App | undefined
let host: HTMLDivElement
let props: { modelValue: boolean; productId: number | null; siteCurrency: string }
const onSuccess = vi.fn()
const onModelUpdate = vi.fn()
const settled = async () => { await Promise.resolve(); await nextTick(); await Promise.resolve(); await nextTick() }
const productResponse = () => ({ data: { data: {
  id: 5,
  title: { 'zh-CN': 'API price test product' },
  skus: [
    { id: 9, sku_code: 'month', spec_values: {}, price_amount: 150, api_supply_price_amount: '123.40', reseller_supply_price_amount: 135, reseller_min_price_amount: 145 },
    { id: 10, sku_code: 'year', spec_values: {}, price_amount: 1600, reseller_supply_price_amount: 1400, reseller_min_price_amount: 1500 },
  ],
} } })
const load = async () => {
  host = document.createElement('div')
  document.body.append(host)
  props = reactive({ modelValue: true, productId: 5 as number | null, siteCurrency: 'CNY' })
  app = createApp({ render: () => h(ProductApiPricingModal, { ...props, onSuccess, 'onUpdate:modelValue': onModelUpdate }) })
  app.mount(host)
  await settled()
}
const input = async (id: string, value: string) => {
  const element = host.querySelector<HTMLInputElement>(`#${id}`)!
  element.value = value
  element.dispatchEvent(new Event('input', { bubbles: true }))
  await settled()
}
const saveButton = () => {
  const buttons = host.querySelectorAll('button')
  return buttons[buttons.length - 1]!
}

beforeEach(() => {
  api.getProduct.mockResolvedValue(productResponse())
  api.updateProductApiPrices.mockResolvedValue({ data: {} })
  auth.hasPermission.mockReturnValue(true)
})
afterEach(() => { app?.unmount(); host?.remove(); vi.clearAllMocks() })

describe('admin API supply pricing dialog', () => {
  it('loads each API price and saves only SKU IDs and normalized API supply prices, then refreshes and closes', async () => {
    await load()
    expect(api.getProduct).toHaveBeenCalledWith(5)
    expect(host.querySelector<HTMLInputElement>('#api-supply-9')?.value).toBe('123.40')
    expect(host.querySelector<HTMLInputElement>('#api-supply-10')?.value).toBe('0.00')
    expect(host.textContent).toContain('150.00')
    await input('api-supply-9', '127.5')
    await input('api-supply-10', '1300')
    saveButton().click()
    await settled()
    expect(api.updateProductApiPrices).toHaveBeenCalledExactlyOnceWith(5, { skus: [
      { id: 9, api_supply_price_amount: '127.50' },
      { id: 10, api_supply_price_amount: '1300.00' },
    ] })
    expect(onSuccess).toHaveBeenCalledOnce()
    expect(onModelUpdate).toHaveBeenCalledExactlyOnceWith(false)
  })

  it('rejects empty, negative, non-decimal and excessive precision values while accepting zero inheritance', async () => {
    await load()
    for (const invalid of ['', '-1', '123.001', '1e2', 'NaN', '1234567890123456789']) {
      await input('api-supply-9', invalid)
      expect(saveButton().disabled).toBe(true)
      expect(host.querySelector('#api-supply-9')?.getAttribute('aria-invalid')).toBe('true')
      saveButton().click()
    }
    expect(api.updateProductApiPrices).not.toHaveBeenCalled()
    await input('api-supply-9', '0')
    expect(saveButton().disabled).toBe(false)
    saveButton().click()
    await settled()
    expect(api.updateProductApiPrices.mock.calls[0]?.[1].skus[0].api_supply_price_amount).toBe('0.00')
  })

  it('preserves server-supported decimal precision without rounding through JavaScript numbers', async () => {
    await load()
    await input('api-supply-9', '999999999999999999.99')
    expect(saveButton().disabled).toBe(false)
    saveButton().click()
    await settled()
    expect(api.updateProductApiPrices.mock.calls[0]?.[1].skus[0].api_supply_price_amount).toBe('999999999999999999.99')
  })

  it('keeps the dialog open and edited values available after a rejected save, then permits retry', async () => {
    api.updateProductApiPrices.mockRejectedValueOnce(new Error('SKU changed; refresh and retry'))
    await load()
    await input('api-supply-9', '129.50')
    saveButton().click()
    await settled()
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('SKU changed')
    expect(host.querySelector<HTMLInputElement>('#api-supply-9')?.value).toBe('129.50')
    expect(saveButton().disabled).toBe(false)
    expect(onSuccess).not.toHaveBeenCalled()
    expect(onModelUpdate).not.toHaveBeenCalled()
    expect(notifySuccess).not.toHaveBeenCalled()
    saveButton().click()
    await settled()
    expect(onSuccess).toHaveBeenCalledOnce()
  })

  it('requires permission for the dedicated API pricing route', async () => {
    auth.hasPermission.mockReturnValue(false)
    await load()
    expect(auth.hasPermission).toHaveBeenCalledWith('PUT:/admin/products/:id/api-prices')
    expect(saveButton().disabled).toBe(true)
    saveButton().click()
    expect(api.updateProductApiPrices).not.toHaveBeenCalled()
  })

  it('blocks saving when loading fails or no SKUs are returned', async () => {
    api.getProduct.mockRejectedValueOnce(new Error('Product unavailable'))
    await load()
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Product unavailable')
    expect(saveButton().disabled).toBe(true)
    api.getProduct.mockResolvedValueOnce({ data: { data: { id: 6, title: {}, skus: [] } } })
    props.productId = 6
    await settled()
    expect(host.textContent).toContain('apiPricing.empty')
    expect(saveButton().disabled).toBe(true)
    expect(api.updateProductApiPrices).not.toHaveBeenCalled()
  })

  it('ignores an older product response after the selected product changes', async () => {
    let resolveOld!: (value: ReturnType<typeof productResponse>) => void
    api.getProduct.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    await load()
    expect(saveButton().disabled).toBe(true)
    api.getProduct.mockResolvedValueOnce({ data: { data: { id: 6, title: {}, skus: [
      { id: 11, sku_code: 'new-product', spec_values: {}, price_amount: 200, api_supply_price_amount: '180.00' },
    ] } } })
    props.productId = 6
    await settled()
    resolveOld(productResponse())
    await settled()
    expect(host.querySelector('#api-supply-9')).toBeNull()
    expect(host.querySelector<HTMLInputElement>('#api-supply-11')?.value).toBe('180.00')
    saveButton().click()
    await settled()
    expect(api.updateProductApiPrices).toHaveBeenCalledExactlyOnceWith(6, { skus: [{ id: 11, api_supply_price_amount: '180.00' }] })
  })
})
