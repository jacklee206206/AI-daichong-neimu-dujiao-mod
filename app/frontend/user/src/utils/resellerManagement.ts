// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

type ResellerManagementProfile = {
  status?: string
}

type ResellerManagementSnapshot = {
  opened?: boolean
  can_apply?: boolean
  profile?: ResellerManagementProfile | null
}

export type ResellerManagementState = {
  canApply: boolean
  canSubmitDomain: boolean
  statusKey: string
}

const RESELLER_PROFILE_STATUS_PENDING_REVIEW = 'pending_review'
const RESELLER_PROFILE_STATUS_ACTIVE = 'active'
const RESELLER_PROFILE_STATUS_REJECTED = 'rejected'
const RESELLER_PROFILE_STATUS_DISABLED = 'disabled'

const RESELLER_DOMAIN_STATUS_PENDING_REVIEW = 'pending_review'
const RESELLER_DOMAIN_STATUS_ACTIVE = 'active'
const RESELLER_DOMAIN_STATUS_DISABLED = 'disabled'

export const getResellerProfileStatusKey = (status?: string) => {
  if (status === RESELLER_PROFILE_STATUS_PENDING_REVIEW) return 'pendingReview'
  if (status === RESELLER_PROFILE_STATUS_ACTIVE) return 'active'
  if (status === RESELLER_PROFILE_STATUS_REJECTED) return 'rejected'
  if (status === RESELLER_PROFILE_STATUS_DISABLED) return 'disabled'
  return 'unknown'
}

export const getResellerDomainStatusKey = (status?: string) => {
  if (status === RESELLER_DOMAIN_STATUS_PENDING_REVIEW) return 'pendingReview'
  if (status === RESELLER_DOMAIN_STATUS_ACTIVE) return 'active'
  if (status === RESELLER_DOMAIN_STATUS_DISABLED) return 'disabled'
  return 'unknown'
}

export const isResellerProfileActive = (profile?: ResellerManagementProfile | null) =>
  profile?.status === RESELLER_PROFILE_STATUS_ACTIVE

export const getResellerManagementState = (snapshot?: ResellerManagementSnapshot | null): ResellerManagementState => {
  if (!snapshot) {
    return {
      canApply: false,
      canSubmitDomain: false,
      statusKey: 'unknown',
    }
  }
  if (!snapshot.opened) {
    return {
      canApply: snapshot.can_apply === true,
      canSubmitDomain: false,
      statusKey: 'notOpened',
    }
  }

  const active = isResellerProfileActive(snapshot.profile)
  return {
    canApply: snapshot.can_apply === true,
    canSubmitDomain: active,
    statusKey: getResellerProfileStatusKey(snapshot.profile?.status),
  }
}

type ResellerDomainReadiness = {
  status?: string
  verification_status?: string
  type?: string
  tls_status?: string
  tls_ready_at?: string | null
  auto_connect_requested_at?: string | null
  last_dns_error?: string
  last_tls_error?: string
}

export const isResellerDomainAccessible = (domain: ResellerDomainReadiness) => {
  if (domain.status !== 'active' || domain.verification_status !== 'verified') return false
  // System subdomains use the operator's preconfigured wildcard DNS/TLS and
  // intentionally have no per-domain TLS verification timestamp.
  if (domain.type === 'subdomain') return true
  return domain.type === 'custom' && domain.tls_status === 'ready' &&
    typeof domain.tls_ready_at === 'string' && Number.isFinite(Date.parse(domain.tls_ready_at))
}

export const getResellerDomainConnectionState = (domain: ResellerDomainReadiness) => {
  if (isResellerDomainAccessible(domain)) return 'active'
  if (domain.status === 'disabled') return 'disabled'
  if (!domain.auto_connect_requested_at) {
    if (domain.status === 'active') return 'approved_pending'
    if (domain.status === 'pending_review') return 'pending_review'
    return 'unknown'
  }
  if (domain.last_tls_error || domain.tls_status === 'failed' || domain.verification_status === 'failed') return 'failed'
  // Missing or propagating DNS is an expected pending step, not a terminal failure.
  // Keep the server's diagnostic visible while the verification worker retries.
  if (domain.last_dns_error) return 'dns_pending'
  if (domain.verification_status === 'verified') return 'tls_pending'
  return 'dns_pending'
}

export const isResellerDomainPendingConnection = (domain: ResellerDomainReadiness) =>
  !isResellerDomainAccessible(domain) && ['active', 'pending_review'].includes(domain.status || '')
