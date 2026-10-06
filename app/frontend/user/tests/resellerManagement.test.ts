// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import test from 'node:test'
import assert from 'node:assert/strict'
import {
  getResellerDomainStatusKey,
  getResellerManagementState,
  getResellerProfileStatusKey,
  isResellerProfileActive,
  isResellerDomainAccessible,
  getResellerDomainConnectionState,
  isResellerDomainPendingConnection,
} from '../src/utils/resellerManagement.ts'

test('user reseller profile status keys map backend values', () => {
  assert.equal(getResellerProfileStatusKey('pending_review'), 'pendingReview')
  assert.equal(getResellerProfileStatusKey('active'), 'active')
  assert.equal(getResellerProfileStatusKey('rejected'), 'rejected')
  assert.equal(getResellerProfileStatusKey('disabled'), 'disabled')
  assert.equal(getResellerProfileStatusKey('unexpected'), 'unknown')
})

test('user reseller domain status keys map backend values', () => {
  assert.equal(getResellerDomainStatusKey('pending_review'), 'pendingReview')
  assert.equal(getResellerDomainStatusKey('active'), 'active')
  assert.equal(getResellerDomainStatusKey('disabled'), 'disabled')
  assert.equal(getResellerDomainStatusKey('unexpected'), 'unknown')
})

test('user reseller management state drives onboarding and domain forms', () => {
  assert.deepEqual(getResellerManagementState(null), {
    canApply: false,
    canSubmitDomain: false,
    statusKey: 'unknown',
  })
  assert.deepEqual(getResellerManagementState({ opened: false, can_apply: true }), {
    canApply: true,
    canSubmitDomain: false,
    statusKey: 'notOpened',
  })
  assert.deepEqual(
    getResellerManagementState({
      opened: true,
      can_apply: false,
      profile: { status: 'pending_review' },
    }),
    { canApply: false, canSubmitDomain: false, statusKey: 'pendingReview' },
  )
  assert.deepEqual(
    getResellerManagementState({
      opened: true,
      can_apply: true,
      profile: { status: 'rejected' },
    }),
    { canApply: true, canSubmitDomain: false, statusKey: 'rejected' },
  )
  assert.deepEqual(
    getResellerManagementState({
      opened: true,
      can_apply: false,
      profile: { status: 'active' },
    }),
    { canApply: false, canSubmitDomain: true, statusKey: 'active' },
  )
})

test('active reseller profile check is strict', () => {
  assert.equal(isResellerProfileActive({ status: 'active' }), true)
  assert.equal(isResellerProfileActive({ status: 'pending_review' }), false)
  assert.equal(isResellerProfileActive(null), false)
})

const customDomain = { type: 'custom', status: 'active', verification_status: 'verified' }
const tlsProof = { tls_status: 'ready', tls_ready_at: '2026-10-02T00:00:00Z' }

test('manual approval without HTTPS evidence remains pending and cannot become an accessible primary domain', () => {
  const approvedOnly = { ...customDomain, is_primary: true, tls_status: 'pending', tls_ready_at: null, auto_connect_requested_at: null }
  assert.equal(isResellerDomainAccessible(approvedOnly), false)
  assert.equal(getResellerDomainConnectionState(approvedOnly), 'approved_pending')
  assert.equal(isResellerDomainPendingConnection(approvedOnly), true)
  const accessibleDomains = [approvedOnly, { ...customDomain, ...tlsProof, is_primary: false }].filter(isResellerDomainAccessible)
  assert.equal(accessibleDomains.length, 1)
  assert.equal(accessibleDomains[0]?.is_primary, false)
})

test('custom domain accessibility requires approval, DNS verification and a recorded HTTPS verification', () => {
  assert.equal(isResellerDomainAccessible({ ...customDomain, ...tlsProof }), true)
  assert.equal(isResellerDomainAccessible({ ...customDomain, ...tlsProof, auto_connect_requested_at: '2026-10-01T00:00:00Z' }), true)
  for (const tls_ready_at of [undefined, null, '', 'not-a-date']) {
    assert.equal(isResellerDomainAccessible({ ...customDomain, tls_status: 'ready', tls_ready_at }), false)
  }
  for (const tls_status of [undefined, 'pending', 'provisioned', 'failed']) {
    assert.equal(isResellerDomainAccessible({ ...customDomain, ...tlsProof, tls_status }), false)
  }
  for (const status of ['pending_review', 'disabled', 'unexpected']) {
    assert.equal(isResellerDomainAccessible({ ...customDomain, ...tlsProof, status }), false)
  }
  assert.equal(isResellerDomainAccessible({ ...customDomain, ...tlsProof, verification_status: 'pending' }), false)
  assert.equal(isResellerDomainAccessible({ ...customDomain, ...tlsProof, type: undefined }), false)
})

test('operator-provisioned system subdomains retain their wildcard HTTPS behavior', () => {
  const systemDomain = { type: 'subdomain', status: 'active', verification_status: 'verified', tls_status: 'pending', tls_ready_at: null }
  assert.equal(isResellerDomainAccessible(systemDomain), true)
  assert.equal(getResellerDomainConnectionState(systemDomain), 'active')
  assert.equal(isResellerDomainPendingConnection(systemDomain), false)
  assert.equal(isResellerDomainAccessible({ ...systemDomain, status: 'pending_review' }), false)
  assert.equal(isResellerDomainAccessible({ ...systemDomain, status: 'disabled' }), false)
  assert.equal(isResellerDomainAccessible({ ...systemDomain, verification_status: 'pending' }), false)
})

test('connection states distinguish review, manual setup, automatic DNS and HTTPS failures', () => {
  assert.equal(getResellerDomainConnectionState({ ...customDomain, status: 'pending_review' }), 'pending_review')
  assert.equal(getResellerDomainConnectionState({ ...customDomain, status: 'disabled' }), 'disabled')
  assert.equal(isResellerDomainPendingConnection({ ...customDomain, status: 'disabled' }), false)
  const automatic = { ...customDomain, auto_connect_requested_at: '2026-10-01T00:00:00Z' }
  assert.equal(getResellerDomainConnectionState({ ...automatic, verification_status: 'pending' }), 'dns_pending')
  assert.equal(getResellerDomainConnectionState({ ...automatic, tls_status: 'ready', tls_ready_at: null }), 'tls_pending')
  for (const failure of [{ verification_status: 'failed' }, { last_tls_error: 'HTTPS probe failed' }, { tls_status: 'failed' }]) {
    assert.equal(getResellerDomainConnectionState({ ...automatic, ...failure }), 'failed')
  }
  assert.equal(getResellerDomainConnectionState({ ...automatic, ...tlsProof }), 'active')
})


test('initial DNS diagnostics stay pending while the worker waits for records and propagation', () => {
  const automatic = { ...customDomain, status: 'pending_review', auto_connect_requested_at: '2026-10-02T00:00:00Z', tls_status: 'pending', verification_status: 'pending' }
  for (const last_dns_error of ['尚未查到 TXT 记录，请核对记录并等待 DNS 生效后重试。', 'TXT 验证值不匹配，请使用该域名页面显示的专属验证值。', '尚未查到网站解析，请添加指向统一入口的 CNAME 后等待生效。']) {
    assert.equal(getResellerDomainConnectionState({ ...automatic, last_dns_error }), 'dns_pending')
    assert.equal(isResellerDomainAccessible({ ...automatic, last_dns_error }), false)
  }
  assert.equal(getResellerDomainConnectionState({ ...automatic, last_dns_error: 'DNS pending', tls_status: 'failed' }), 'failed')
  assert.equal(getResellerDomainConnectionState({ ...automatic, last_dns_error: 'DNS pending', verification_status: 'failed' }), 'failed')
  assert.equal(getResellerDomainConnectionState({ ...automatic, last_dns_error: '', verification_status: 'verified' }), 'tls_pending')
})
