// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive, type App } from 'vue'
import PaymentChannelModal from './PaymentChannelModal.vue'

const api = vi.hoisted(() => ({
  getMemberLevels: vi.fn(),
  getPaymentChannel: vi.fn(),
  updatePaymentChannel: vi.fn(),
  createPaymentChannel: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ getLocalizedText: (value: Record<string, string>) => value['zh-CN'] }))
vi.mock('@/components/admin/MediaPicker.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/ui/dialog', async () => {
  const { defineComponent, h } = await import('vue')
  const container = defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) })
  return { Dialog: container, DialogScrollContent: container, DialogHeader: container, DialogTitle: container }
})

const channel = (id: number, rate = '3.2') => ({
  id,
  name: id === 1 ? 'Alipay' : 'WeChat',
  icon: '/uploads/payment-icon.png',
  provider_type: 'epay',
  channel_type: id === 1 ? 'alipay' : 'wechat',
  interaction_mode: 'redirect',
  fee_rate: '0',
  fixed_fee: '0',
  min_amount: '1',
  max_amount: '3000',
  hide_amount_out_range: true,
  payment_types: ['order'],
  payment_roles: ['guest', 'member'],
  member_levels: [2],
  is_active: false,
  sort_order: 20,
  config_json: {
    epay_version: 'v1',
    gateway_url: 'https://pay.example.test/',
    merchant_id: 'merchant-fixture',
    merchant_key: '******',
    notify_url: 'https://shop.example.test/api/payment/notify',
    return_url: 'https://shop.example.test/payment/result',
    gateway_customer_fee_rate: rate,
    gateway_customer_fixed_fee: '0',
    custom_provider_option: { preserve: true },
  },
})
const response = (id: number, rate = '3.2') => ({ data: { data: channel(id, rate) } })
const deferred = () => {
  let resolve!: (value: ReturnType<typeof response>) => void
  const promise = new Promise<ReturnType<typeof response>>(done => { resolve = done })
  return { promise, resolve }
}

let app: App | undefined
let host: HTMLDivElement
let state: { modelValue: boolean; channelId: number | null; focusGatewayFee: boolean }
const settled = async () => {
  for (let i = 0; i < 5; i++) { await Promise.resolve(); await nextTick() }
}
const mount = async (focusGatewayFee = true) => {
  state = reactive({ modelValue: true, channelId: 1, focusGatewayFee })
  host = document.createElement('div')
  document.body.append(host)
  app = createApp({ render: () => h(PaymentChannelModal, {
    ...state,
    'onUpdate:modelValue': (value: boolean) => { state.modelValue = value },
  }) })
  app.mount(host)
  await settled()
}
const rateInput = () => host.querySelector<HTMLInputElement>('#gateway-customer-fee-rate')!
const changeRate = async (value: string) => {
  rateInput().value = value
  rateInput().dispatchEvent(new Event('input', { bubbles: true }))
  await settled()
}
const submit = async () => {
  host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
  await settled()
}

beforeEach(() => {
  api.getMemberLevels.mockResolvedValue({ data: { data: [] } })
  api.getPaymentChannel.mockImplementation((id: number) => Promise.resolve(response(id)))
  api.updatePaymentChannel.mockResolvedValue({ data: {} })
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() })
})
afterEach(() => {
  app?.unmount()
  host?.remove()
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

describe('payment channel customer fee shortcut', () => {
  it('focuses the loaded external fee and preserves channel settings and masked config when saving', async () => {
    await mount()
    expect(document.activeElement).toBe(rateInput())
    await changeRate('2.5')
    await submit()
    const expected = channel(1)
    const { id: _id, ...payload } = expected
    expect(api.updatePaymentChannel).toHaveBeenCalledWith(1, {
      ...payload,
      config_json: { ...expected.config_json, gateway_customer_fee_rate: '2.5' },
    })
    expect(state.modelValue).toBe(false)
  })

  it('waits for details, ignores late responses, and reloads the same channel after canceling', async () => {
    const first = deferred()
    api.getPaymentChannel.mockReturnValueOnce(first.promise)
    await mount()
    expect(host.querySelector('form')).toBeNull()
    expect(host.querySelector('[role="status"]')).not.toBeNull()
    state.channelId = 2
    await settled()
    expect(host.textContent).toContain('WeChat')
    first.resolve(response(1, '9'))
    await settled()
    expect(rateInput().value).toBe('3.2')
    expect(host.textContent).toContain('WeChat')
    await changeRate('8')
    state.modelValue = false
    await settled()
    state.modelValue = true
    await settled()
    expect(api.getPaymentChannel.mock.calls.map(call => call[0])).toEqual([1, 2, 2])
    expect(rateInput().value).toBe('3.2')
    await submit()
    expect(api.updatePaymentChannel.mock.calls[0]?.[0]).toBe(2)
    expect(api.updatePaymentChannel.mock.calls[0]?.[1].channel_type).toBe('wechat')
  })

  it('hides the previous channel form if loading a different channel fails', async () => {
    await mount()
    api.getPaymentChannel.mockRejectedValueOnce(new Error('Could not load channel'))
    state.channelId = 2
    await settled()
    expect(host.querySelector('form')).toBeNull()
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Could not load channel')
    expect(api.updatePaymentChannel).not.toHaveBeenCalled()
  })

  it('keeps normal editing available and retains the entered rate after a save failure', async () => {
    await mount(false)
    expect(document.activeElement).not.toBe(rateInput())
    expect(host.querySelectorAll('button[type="submit"]')).toHaveLength(1)
    api.updatePaymentChannel.mockRejectedValueOnce(new Error('Save failed'))
    await changeRate('1.8')
    await submit()
    expect(state.modelValue).toBe(true)
    expect(rateInput().value).toBe('1.8')
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Save failed')
    expect(host.querySelector<HTMLButtonElement>('button[type="submit"]')?.disabled).toBe(false)
    await submit()
    expect(state.modelValue).toBe(false)
  })
})
