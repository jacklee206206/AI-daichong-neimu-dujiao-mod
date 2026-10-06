// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import ApiPanel from '../src/views/personal/ApiPanel.vue'

const api = vi.hoisted(() => ({ getMy: vi.fn(), apply: vi.fn(), regenerate: vi.fn(), updateStatus: vi.fn() }))
vi.mock('../src/api', () => ({ apiCredentialAPI: api }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

let app: App | undefined
let host: HTMLDivElement
const settled = async () => { for (let i = 0; i < 4; i += 1) { await Promise.resolve(); await nextTick() } }
const apply = async () => {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(ApiPanel)
  app.mount(host)
  await settled()
  const button = [...host.querySelectorAll('button')].find((item) => item.textContent?.includes('apiPanel.apply'))!
  button.click()
  await settled()
}

beforeEach(() => {
  const values = new Map<string, string>()
  vi.stubGlobal('localStorage', { getItem: (key: string) => values.get(key) || null, setItem: (key: string, value: string) => values.set(key, value) })
  api.getMy.mockResolvedValueOnce({ data: { data: { status: 'none' } } })
})
afterEach(() => { app?.unmount(); host?.remove(); vi.clearAllMocks(); vi.unstubAllGlobals() })

describe('API application result presentation', () => {
  it('shows approved access immediately and leaves secret generation to the user', async () => {
    api.apply.mockResolvedValue({ data: { data: { id: 1, status: 'approved' } } })
    api.getMy.mockResolvedValueOnce({ data: { data: { id: 1, status: 'approved', api_key: 'test-key', is_active: true } } })
    await apply()
    expect(host.textContent).toContain('apiPanel.applyApproved')
    expect(host.textContent).toContain('apiPanel.generateSecret')
    expect(host.textContent).not.toContain('apiPanel.pendingDesc')
    expect(api.regenerate).not.toHaveBeenCalled()
  })

  it('keeps manual review pending and preserves the result alert after loading', async () => {
    api.apply.mockResolvedValue({ data: { data: { id: 1, status: 'pending_review' } } })
    api.getMy.mockResolvedValueOnce({ data: { data: { id: 1, status: 'pending_review', is_active: false } } })
    await apply()
    expect(host.textContent).toContain('apiPanel.applySuccess')
    expect(host.textContent).toContain('apiPanel.pendingDesc')
    expect(host.textContent).not.toContain('apiPanel.generateSecret')
    expect(api.regenerate).not.toHaveBeenCalled()
  })

  it('offers a status reload after a failed refresh without asking the user to apply again', async () => {
    api.apply.mockResolvedValue({ data: { data: { id: 1, status: 'approved' } } })
    api.getMy.mockRejectedValueOnce(new Error('Status temporarily unavailable'))
    await apply()
    expect(host.textContent).toContain('apiPanel.loadFailed')
    expect(host.textContent).toContain('apiPanel.reloadStatus')
    expect(host.textContent).not.toContain('apiPanel.noCredential')
    expect(api.apply).toHaveBeenCalledTimes(1)
    expect(api.regenerate).not.toHaveBeenCalled()
  })
})
