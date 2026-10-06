// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, nextTick, type App } from 'vue'
import ApiCredentialReviewSettings from './ApiCredentialReviewSettings.vue'

const api = vi.hoisted(() => ({ getSettings: vi.fn(), updateSettings: vi.fn() }))
const permissions = vi.hoisted(() => ({ read: true, write: true }))
vi.mock('@/api/admin', () => ({ adminAPI: api }))
vi.mock('@/stores/auth', () => ({ useAdminAuthStore: () => ({ hasPermission: (key: string) => key.startsWith('GET:') ? permissions.read : permissions.write }) }))
vi.mock('@/utils/notify', () => ({ notifySuccess: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

let app: App | undefined
let host: HTMLDivElement
const saved = vi.fn()
const settled = async () => { for (let i = 0; i < 3; i += 1) { await Promise.resolve(); await nextTick() } }
const load = async () => {
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(ApiCredentialReviewSettings, { onSaved: saved })
  app.mount(host)
  await settled()
}
const saveButton = () => [...host.querySelectorAll('button')].find((button) => button.textContent?.includes('reviewSettings.save'))!

beforeEach(() => {
  permissions.read = true
  permissions.write = true
  api.getSettings.mockResolvedValue({ data: { data: { auto_approve_applications: false, future_option: 'keep' } } })
  api.updateSettings.mockResolvedValue({ data: { data: {} } })
})
afterEach(() => { app?.unmount(); host?.remove(); vi.clearAllMocks() })

describe('API credential review settings', () => {
  it('saves both automatic and manual modes, preserving other settings and saved status', async () => {
    await load()
    expect(api.getSettings).toHaveBeenCalledWith({ key: 'api_credential_config' })
    host.querySelector<HTMLButtonElement>('[role="switch"]')!.click()
    await settled()
    expect(host.querySelector('[role="status"]')?.textContent).toContain('reviewSettings.manual')
    saveButton().click()
    await settled()
    expect(api.updateSettings).toHaveBeenLastCalledWith({ key: 'api_credential_config', value: { auto_approve_applications: true, future_option: 'keep' } })
    expect(host.querySelector('[role="status"]')?.textContent).toContain('reviewSettings.automatic')
    expect(saved).toHaveBeenCalledTimes(1)
    host.querySelector<HTMLButtonElement>('[role="switch"]')!.click()
    await settled()
    saveButton().click()
    await settled()
    expect(api.updateSettings.mock.calls[1]?.[0].value.auto_approve_applications).toBe(false)
  })

  it('blocks saving after a read failure instead of overwriting with defaults', async () => {
    api.getSettings.mockRejectedValueOnce(new Error('Read failed'))
    await load()
    expect(saveButton().disabled).toBe(true)
    expect(host.querySelector<HTMLButtonElement>('[role="switch"]')?.disabled).toBe(true)
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Read failed')
    expect(api.updateSettings).not.toHaveBeenCalled()
  })

  it('does not claim the selected mode is saved after a write failure', async () => {
    api.updateSettings.mockRejectedValueOnce(new Error('Save failed'))
    await load()
    host.querySelector<HTMLButtonElement>('[role="switch"]')!.click()
    await settled()
    saveButton().click()
    await settled()
    expect(host.querySelector('[role="status"]')).toBeNull()
    expect(saveButton().disabled).toBe(true)
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Save failed')
    expect(saved).not.toHaveBeenCalled()
  })

  it('uses the existing settings write permission', async () => {
    permissions.write = false
    await load()
    expect(saveButton().disabled).toBe(true)
    expect(host.querySelector<HTMLButtonElement>('[role="switch"]')?.disabled).toBe(true)
  })

  it('does not read or render settings without the existing read permission', async () => {
    permissions.read = false
    await load()
    expect(api.getSettings).not.toHaveBeenCalled()
    expect(host.querySelector('section')).toBeNull()
  })
})
