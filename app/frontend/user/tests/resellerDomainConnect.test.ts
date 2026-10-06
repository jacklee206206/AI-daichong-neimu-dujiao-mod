// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import test from 'node:test'
import assert from 'node:assert/strict'
import { canAuthorizeDomainConnection, canRestartDomainConnection, canStartManualConnection, clearDomainCredentials, domainConnectionComplete, domainConnectionShouldPoll, emptyDomainCredentials, takeDomainCredentials, validDomainConnectInput, validManualDomainInput } from '../src/utils/resellerDomainConnect.ts'
import type { ResellerDomainConnectInspection, ResellerDomainConnectResult } from '../src/api/types.ts'

const inspection = (overrides = {}): ResellerDomainConnectInspection => ({ root_domain: 'example.com', provider: 'cloudflare', provider_label: 'Cloudflare', supported: true, domains: ['example.com', 'www.example.com'], entry_ip: '1.1.1.1', required_permissions: [], conflicts: [], instructions: [], ...overrides })

test('domain input blocks URL, IP, www and malformed labels before inspection', () => {
  for (const domain of [' example.com ', 'EXAMPLE.SHOP', 'example.co.uk', 'xn--fsqu00a.com']) assert.equal(validDomainConnectInput(domain), true, domain)
  for (const domain of ['https://example.com', 'example.com/path', 'example.com:443', 'example.com?x=1', 'example.com#x', '127.0.0.1', '[::1]', 'www.example.com', 'example', '-example.com', 'example-.com', 'bad_domain.com', 'ex ample.com', 'example.com.']) assert.equal(validDomainConnectInput(domain), false, domain)
})

test('unsupported providers and conflicts cannot authorize DNS mutations', () => {
  assert.equal(canAuthorizeDomainConnection(inspection()), true)
  assert.equal(canAuthorizeDomainConnection(inspection({ supported: false })), false)
  assert.equal(canAuthorizeDomainConnection(inspection({ provider: 'unsupported' })), false)
  assert.equal(canAuthorizeDomainConnection(inspection({ provider: 'unknown' })), false)
  assert.equal(canAuthorizeDomainConnection(inspection({ conflicts: ['Another shop owns www.example.com'] })), false)
  assert.equal(canAuthorizeDomainConnection(null), false)
})

test('taking credentials only sends selected provider fields and clears every sensitive field immediately', () => {
  const state = { api_token: ' token ', access_key_id: 'unused-id', access_key_secret: 'unused-secret', secret_id: 'unused', secret_key: 'unused' }
  assert.deepEqual(takeDomainCredentials('cloudflare', state), { api_token: 'token' })
  assert.deepEqual(state, emptyDomainCredentials())
  state.secret_id = ' id '; state.secret_key = ' secret '; state.api_token = 'stale'
  assert.deepEqual(takeDomainCredentials('dnspod', state), { secret_id: 'id', secret_key: 'secret' })
  assert.deepEqual(state, emptyDomainCredentials())
  state.access_key_secret = 'cancelled'; clearDomainCredentials(state)
  assert.deepEqual(state, emptyDomainCredentials())
})

test('SaaS allows any provider to configure records manually only after platform readiness and no conflicts', () => {
  const ready = inspection({ connect_mode: 'cloudflare_saas', platform_ready: true, entry_host: 'entry.example.net', provider: 'unsupported', supported: false })
  assert.equal(canStartManualConnection(ready), true)
  assert.equal(canAuthorizeDomainConnection(ready), false)
  for (const platform_ready of [false, undefined]) {
    assert.equal(canStartManualConnection({ ...ready, platform_ready }), false)
    assert.equal(canAuthorizeDomainConnection(inspection({ connect_mode: 'cloudflare_saas', platform_ready })), false)
  }
  assert.equal(canStartManualConnection({ ...ready, conflicts: ['Already assigned'] }), false)
  assert.equal(canStartManualConnection({ ...ready, entry_host: '' }), false)
  assert.equal(canStartManualConnection(inspection()), false)
})

test('connection only reports success after both verified hosts have ready HTTPS', () => {
  const ready = { id: 1, domain: 'example.com', type: 'custom', status: 'active', verification_status: 'verified', tls_status: 'ready', tls_ready_at: '2026-10-02T00:00:00Z', is_primary: true, created_at: '', updated_at: '' }
  const result: ResellerDomainConnectResult = { domains: [ready, { ...ready, id: 2, domain: 'www.example.com' }], phase: 'active', message: '' }
  assert.equal(domainConnectionComplete(result), true)
  assert.equal(domainConnectionComplete({ ...result, domains: [ready] }), false)
  assert.equal(domainConnectionComplete({ ...result, domains: [ready, { ...ready, tls_status: 'provisioned' }] }), false)
  assert.equal(domainConnectionComplete({ ...result, domains: [ready, { ...ready, verification_status: 'pending' }] }), false)
  assert.equal(domainConnectionComplete({ ...result, domains: [ready, { ...ready, tls_ready_at: undefined }] }), false)
  assert.equal(domainConnectionComplete({ ...result, domains: [ready, { ...ready, tls_status: 'pending', tls_ready_at: undefined }] }), false)
  assert.equal(domainConnectionComplete({ ...result, phase: 'tls_pending' }), false)
  for (const phase of ['dns_pending', 'edge_pending', 'tls_pending'] as const) assert.equal(domainConnectionShouldPoll({ ...result, phase }), true)
  for (const phase of ['failed', 'manual_required', 'waiting_authorization', 'active'] as const) assert.equal(domainConnectionShouldPoll({ ...result, phase }), false)
})


test('manual domain entry retains subdomains but rejects URLs, IPs and wildcards', () => {
  for (const domain of ['shop.example.com', 'www.example.com', 'example.com', 'xn--fsqu00a.com']) assert.equal(validManualDomainInput(domain), true)
  for (const domain of ['https://shop.example.com', 'shop.example.com/path', '*.example.com', '127.0.0.1', 'shop.example.com:443']) assert.equal(validManualDomainInput(domain), false)
})


test('restarting a legacy binding requires server eligibility, explicit confirmation and a ready SaaS platform', () => {
  const ready = inspection({ connect_mode: 'cloudflare_saas', platform_ready: true, entry_host: 'entry.example.net', can_restart: true, conflicts: ['Existing legacy binding must be reconnected'] })
  assert.equal(canRestartDomainConnection(ready, true), true)
  assert.equal(canRestartDomainConnection(ready, false), false)
  assert.equal(canRestartDomainConnection(null, true), false)
  for (const can_restart of [false, undefined]) assert.equal(canRestartDomainConnection({ ...ready, can_restart }, true), false)
  for (const platform_ready of [false, undefined]) assert.equal(canRestartDomainConnection({ ...ready, platform_ready }, true), false)
  assert.equal(canRestartDomainConnection({ ...ready, connect_mode: 'legacy' }, true), false)
  assert.equal(canRestartDomainConnection({ ...ready, entry_host: '' }, true), false)
  // Even a conflict-free legacy binding must use the explicit restart action.
  const noConflict = { ...ready, conflicts: [] }
  assert.equal(canAuthorizeDomainConnection(noConflict), false)
  assert.equal(canStartManualConnection(noConflict), false)
})
