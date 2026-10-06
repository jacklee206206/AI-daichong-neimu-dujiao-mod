// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import type { ResellerDNSProvider, ResellerDomainConnectInspection, ResellerDomainConnectResult } from '../api/types.ts'
import { isResellerDomainAccessible } from './resellerManagement.ts'

export const domainCredentialFields = {
  cloudflare: ['api_token'],
  aliyun: ['access_key_id', 'access_key_secret'],
  dnspod: ['secret_id', 'secret_key'],
  unsupported: [],
} as const

export type DomainCredentialField = 'api_token' | 'access_key_id' | 'access_key_secret' | 'secret_id' | 'secret_key'
export type DomainCredentials = Record<DomainCredentialField, string>

export const emptyDomainCredentials = (): DomainCredentials => ({
  api_token: '', access_key_id: '', access_key_secret: '', secret_id: '', secret_key: '',
})

export const clearDomainCredentials = (credentials: DomainCredentials) => {
  for (const key of Object.keys(credentials) as DomainCredentialField[]) credentials[key] = ''
}

// Only the selected provider's fields enter the request; erase all form fields immediately.
export const takeDomainCredentials = (provider: ResellerDNSProvider, credentials: DomainCredentials) => {
  const payload: Record<string, string> = {}
  for (const key of domainCredentialFields[provider] || []) payload[key] = credentials[key].trim()
  clearDomainCredentials(credentials)
  return payload
}

// The server additionally checks the public suffix and registrable root domain.
export const validManualDomainInput = (input: string) => {
  const domain = input.trim().toLowerCase()
  if (!domain || domain.length > 253 || /[\s/:?#@\\]/.test(domain)) return false
  const labels = domain.split('.')
  return labels.length >= 2 && !/^\d+$/.test(labels[labels.length - 1] || '') &&
    labels.every((label) => label.length > 0 && label.length <= 63 && /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label))
}

export const validDomainConnectInput = (input: string) =>
  validManualDomainInput(input) && !input.trim().toLowerCase().startsWith('www.')

export const canAuthorizeDomainConnection = (inspection: ResellerDomainConnectInspection | null) =>
  !!inspection && inspection.can_restart !== true && inspection.supported === true &&
  (inspection.connect_mode !== 'cloudflare_saas' || inspection.platform_ready === true) &&
  Object.prototype.hasOwnProperty.call(domainCredentialFields, inspection.provider) && inspection.provider !== 'unsupported' &&
  (inspection.conflicts || []).length === 0

export const canStartManualConnection = (inspection: ResellerDomainConnectInspection | null) =>
  !!inspection && inspection.can_restart !== true && inspection.connect_mode === 'cloudflare_saas' && inspection.platform_ready === true &&
  !!inspection.entry_host && (inspection.conflicts || []).length === 0

// A server-approved restart keeps ownership; it never overwrites the conflicting DNS records.
export const canRestartDomainConnection = (inspection: ResellerDomainConnectInspection | null, confirmed: boolean) =>
  !!inspection && confirmed === true && inspection.can_restart === true &&
  inspection.connect_mode === 'cloudflare_saas' && inspection.platform_ready === true && !!inspection.entry_host

export const domainConnectionComplete = (result: ResellerDomainConnectResult | null) =>
  !!result && result.phase === 'active' && result.domains.length >= 2 &&
  result.domains.every(isResellerDomainAccessible)

export const domainConnectionShouldPoll = (result: ResellerDomainConnectResult | null) =>
  !!result && ['dns_pending', 'edge_pending', 'tls_pending'].includes(result.phase)
