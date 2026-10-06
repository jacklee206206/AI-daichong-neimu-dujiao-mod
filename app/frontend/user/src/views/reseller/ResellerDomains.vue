<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <div class="space-y-5">
    <ResellerSectionHeader
      :title="t('resellerConsole.domains.title')"
      :description="t('resellerConsole.domains.description')"
    >
      <template #actions>
        <Button type="button" variant="outline" size="sm" @click="load">
          <RotateCw class="h-4 w-4" />
          {{ t('orders.filters.refresh') }}
        </Button>
      </template>
    </ResellerSectionHeader>

    <Alert
      v-if="alert"
      :variant="alert.level === 'error' ? 'destructive' : 'default'"
      :class="alert.level === 'success' ? 'border-success/40 text-success' : ''"
    >
      <AlertDescription>{{ alert.message }}</AlertDescription>
    </Alert>

    <ResellerPageState v-if="loading" loading :title="t('resellerConsole.common.loading')" />

    <template v-else>
      <section class="overflow-hidden rounded-xl border border-border bg-card shadow-sm">
        <div class="border-b border-border px-5 py-5 sm:px-7">
          <div class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
            <div class="flex min-w-0 items-start gap-3">
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
                <Globe2 class="h-5 w-5" />
              </span>
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h2 class="text-base font-bold text-foreground">{{ t('resellerConsole.domains.workspaceTitle') }}</h2>
                  <ResellerStatusBadge v-if="primaryDomain" :label="t('resellerConsole.domains.primaryBadge')" tone="accent" />
                </div>
                <p class="mt-1 text-sm text-muted-foreground">{{ t('resellerConsole.domains.workspaceDescription') }}</p>
                <div class="mt-2 break-all font-mono text-sm font-semibold text-foreground">
                  {{ primaryDomain?.domain || t('resellerConsole.domains.noPrimaryDomain') }}
                </div>
              </div>
            </div>

            <div class="grid overflow-hidden rounded-xl border border-border bg-muted/30 sm:grid-cols-3">
              <div class="min-w-0 px-4 py-3">
                <div class="text-xs font-semibold uppercase text-muted-foreground">{{ t('resellerConsole.domains.primaryTitle') }}</div>
                <div class="mt-1 text-lg font-bold text-foreground">{{ primaryDomain ? 1 : 0 }}</div>
              </div>
              <div class="min-w-0 border-t border-border px-4 py-3 sm:border-l sm:border-t-0">
                <div class="text-xs font-semibold uppercase text-muted-foreground">{{ t('resellerConsole.domains.activeTitle') }}</div>
                <div class="mt-1 text-lg font-bold text-success">{{ activeDomains.length }}</div>
              </div>
              <div class="min-w-0 border-t border-border px-4 py-3 sm:border-l sm:border-t-0">
                <div class="text-xs font-semibold uppercase text-muted-foreground">{{ t('resellerConsole.domains.reviewTitle') }}</div>
                <div class="mt-1 text-lg font-bold text-warning">{{ pendingDomains.length }}</div>
              </div>
            </div>
          </div>
        </div>

        <div class="divide-y divide-border">
          <section v-if="systemDomain" class="grid gap-4 px-5 py-5 sm:px-6 lg:grid-cols-[180px_minmax(0,1fr)]">
            <div class="flex items-start gap-3">
              <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                <Link2 class="h-4 w-4" />
              </span>
              <div>
                <h3 class="text-sm font-bold text-foreground">{{ t('resellerConsole.domains.systemDomainTitle') }}</h3>
                <p class="mt-1 text-xs leading-relaxed text-muted-foreground">
                  {{ systemDomain ? t('resellerConsole.domains.systemAssignedDesc') : t('resellerConsole.domains.systemWaitingDesc') }}
                </p>
              </div>
            </div>

            <div v-if="systemDomain" class="min-w-0">
              <div class="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
                <div class="min-w-0">
                  <div class="break-all font-mono text-base font-bold text-foreground">{{ systemDomain.domain }}</div>
                  <div class="mt-2 flex flex-wrap gap-2">
                    <ResellerStatusBadge :label="domainStatusLabel(systemDomain.status)" :tone="domainTone(systemDomain.status)" dot />
                    <ResellerStatusBadge :label="verificationLabel(systemDomain.verification_status)" :tone="verificationTone(systemDomain.verification_status)" />
                    <ResellerStatusBadge :label="domainTypeLabel(systemDomain.type)" tone="neutral" />
                    <ResellerStatusBadge v-if="systemDomain.is_primary && isActiveVerifiedDomain(systemDomain)" :label="t('personalCenter.reseller.primaryDomain')" tone="accent" />
                  </div>
                  <div class="mt-3 text-xs text-muted-foreground">
                    {{ t('personalCenter.reseller.updatedAt') }} {{ formatResellerConsoleDate(systemDomain.updated_at) }}
                  </div>
                </div>
                <div class="flex shrink-0 flex-wrap items-center gap-2">
                  <ResellerCopyButton :value="systemDomain.domain" :label="t('resellerConsole.common.copy')" />
                  <Button v-if="isActiveVerifiedDomain(systemDomain)" as-child variant="ghost" size="sm">
                    <a :href="`https://${systemDomain.domain}`" target="_blank" rel="noopener noreferrer">
                      <ExternalLink class="h-4 w-4" />
                      {{ t('resellerConsole.domains.visit') }}
                    </a>
                  </Button>
                </div>
              </div>
            </div>

            <div v-else class="rounded-lg border border-dashed border-border bg-muted/20 px-4 py-4">
              <div class="flex items-start gap-3">
                <CircleAlert class="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                <div>
                  <h4 class="text-sm font-semibold text-foreground">{{ t('resellerConsole.domains.systemEmptyTitle') }}</h4>
                  <p class="mt-1 text-sm leading-relaxed text-muted-foreground">{{ t('resellerConsole.domains.systemEmptyDescription') }}</p>
                </div>
              </div>
            </div>
          </section>

          <ResellerDomainConnect :enabled="canSubmitDomain" :domains="domains" @domains="mergeDomains" @manual="showSetup" />

          <section>
            <div class="flex flex-col gap-2 px-5 py-4 sm:flex-row sm:items-end sm:justify-between sm:px-6">
              <div>
                <h3 class="text-sm font-bold text-foreground">{{ t('resellerConsole.domains.customDomains') }}</h3>
                <p class="mt-1 text-sm text-muted-foreground">{{ t('resellerConsole.domains.customDescription') }}</p>
              </div>
              <ResellerStatusBadge :label="String(customDomains.length)" tone="neutral" />
            </div>

            <div v-if="customDomains.length > 0" class="border-t border-border">
              <div class="hidden grid-cols-[minmax(0,1.3fr)_130px_minmax(140px,1fr)_140px_160px] gap-3 bg-muted/30 px-5 py-3 text-xs font-semibold uppercase text-muted-foreground sm:px-6 xl:grid">
                <div>{{ t('resellerConsole.domains.tableDomain') }}</div>
                <div>{{ t('resellerConsole.domains.tableProvider') }}</div>
                <div>{{ t('resellerConsole.domains.tableProgress') }}</div>
                <div>{{ t('resellerConsole.domains.tableUpdated') }}</div>
                <div class="text-right">{{ t('resellerConsole.domains.tableActions') }}</div>
              </div>

              <div class="divide-y divide-border">
                <div
                  v-for="item in customDomains"
                  :key="item.id"
                  class="grid gap-3 px-5 py-4 sm:px-6 xl:grid-cols-[minmax(0,1.3fr)_130px_minmax(140px,1fr)_140px_160px] xl:items-center"
                >
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <div class="min-w-0 break-all font-mono text-sm font-bold text-foreground">{{ item.domain }}</div>
                      <ResellerStatusBadge v-if="item.is_primary && isActiveVerifiedDomain(item)" :label="t('personalCenter.reseller.primaryDomain')" tone="accent" />
                    </div>
                    <div class="mt-1 text-xs text-muted-foreground xl:hidden">{{ domainTypeLabel(item.type) }}</div>
                  </div>

                  <div class="flex items-center justify-between gap-3 xl:block">
                    <span class="text-xs font-medium text-muted-foreground xl:hidden">{{ t('resellerConsole.domains.tableProvider') }}</span>
                    <span class="text-sm text-muted-foreground">{{ providerLabel(item.dns_provider) }}</span>
                  </div>

                  <div class="flex items-center justify-between gap-3 xl:block">
                    <span class="text-xs font-medium text-muted-foreground xl:hidden">{{ t('resellerConsole.domains.tableProgress') }}</span>
                    <div class="min-w-0 xl:max-w-56">
                      <ResellerStatusBadge :label="connectionProgress(item).label" :tone="connectionProgress(item).tone" dot />
                      <p v-if="getResellerDomainConnectionState(item) === 'approved_pending'" class="mt-2 text-xs leading-relaxed text-muted-foreground">{{ t('resellerConsole.domains.approvedPendingHint') }}</p>
                      <p v-if="item.auto_connect_requested_at && (item.last_dns_error || item.last_tls_error)" class="mt-2 break-words text-xs" :class="getResellerDomainConnectionState(item) === 'failed' ? 'text-destructive' : 'text-muted-foreground'">{{ item.last_dns_error || item.last_tls_error }}</p>
                    </div>
                  </div>

                  <div class="flex items-center justify-between gap-3 text-xs text-muted-foreground xl:block">
                    <span class="font-medium xl:hidden">{{ t('resellerConsole.domains.tableUpdated') }}</span>
                    <span>{{ formatResellerConsoleDate(item.updated_at) }}</span>
                  </div>

                  <div class="flex flex-wrap items-center gap-2 xl:justify-end">
                    <Button v-if="!isActiveVerifiedDomain(item)" variant="ghost" size="sm" :disabled="checkingDomain !== null" @click="showSetup(item)">{{ t('resellerConsole.domains.manualHelp') }}</Button>
                    <ResellerCopyButton :value="item.domain" :label="t('resellerConsole.common.copy')" />
                    <Button v-if="isActiveVerifiedDomain(item)" as-child variant="ghost" size="sm">
                      <a :href="`https://${item.domain}`" target="_blank" rel="noopener noreferrer">
                        <ExternalLink class="h-4 w-4" />
                        {{ t('resellerConsole.domains.visit') }}
                      </a>
                    </Button>
                  </div>
                </div>
              </div>
            </div>

            <div v-else class="border-t border-border px-5 py-8 text-center sm:px-6">
              <span class="mx-auto flex h-11 w-11 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                <Globe2 class="h-5 w-5" />
              </span>
              <h3 class="mt-3 text-sm font-bold text-foreground">{{ t('personalCenter.reseller.domainEmpty') }}</h3>
              <p class="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">{{ t('resellerConsole.domains.customEmptyDescription') }}</p>
            </div>
          </section>
        </div>
      </section>
      <section v-if="setup" ref="domainSetupSection" class="rounded-xl border border-border bg-card p-5">
        <h2 class="font-semibold">{{ t('resellerConsole.domains.manualHelp') }} · {{ setup.domain.domain }}</h2>
        <ul v-if="setup.instructions?.length" class="mt-3 list-disc space-y-1 pl-4 text-sm leading-relaxed text-muted-foreground"><li v-for="line in setup.instructions" :key="line">{{ line }}</li></ul>
        <p v-else class="mt-3 text-sm leading-relaxed text-muted-foreground">{{ t('resellerConsole.domains.verifyDesc') }}</p>
        <div v-for="record in setup.records" :key="`${record.type}:${record.name}:${record.value}`" class="mt-4 flex flex-wrap items-center gap-3 rounded-lg bg-muted/30 p-3 font-mono text-sm">
          <span>{{ record.type }}</span><span class="break-all">{{ record.name }}</span><ResellerCopyButton :value="record.name" /><span class="break-all">→ {{ record.value }}</span><ResellerCopyButton :value="record.value" />
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { CircleAlert, ExternalLink, Globe2, Link2, RotateCw } from 'lucide-vue-next'
import { resellerAPI, type ResellerDomainData, type ResellerDomainSetupData, type ResellerManagementSnapshotData } from '../../api'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import ResellerDomainConnect from '../../components/reseller/ResellerDomainConnect.vue'
import ResellerCopyButton from '../../components/reseller-console/ResellerCopyButton.vue'
import ResellerPageState from '../../components/reseller-console/ResellerPageState.vue'
import ResellerSectionHeader from '../../components/reseller-console/ResellerSectionHeader.vue'
import ResellerStatusBadge, { type ResellerBadgeTone } from '../../components/reseller-console/ResellerStatusBadge.vue'
import {
  RESELLER_DOMAIN_STATUS_ACTIVE,
  RESELLER_DOMAIN_STATUS_DISABLED,
  RESELLER_DOMAIN_STATUS_PENDING_REVIEW,
  RESELLER_DOMAIN_TYPE_CUSTOM,
  RESELLER_DOMAIN_TYPE_SUBDOMAIN,
  RESELLER_DOMAIN_VERIFICATION_FAILED,
  RESELLER_DOMAIN_VERIFICATION_PENDING,
  RESELLER_DOMAIN_VERIFICATION_VERIFIED,
  RESELLER_PROFILE_STATUS_ACTIVE,
} from '../../constants/reseller'
import { type PageAlert } from '../../utils/alerts'
import { formatResellerConsoleDate } from '../../utils/resellerConsole'
import { getResellerDomainConnectionState, getResellerDomainStatusKey, isResellerDomainAccessible, isResellerDomainPendingConnection } from '../../utils/resellerManagement'

const { t } = useI18n()
const loading = ref(false)
const snapshot = ref<ResellerManagementSnapshotData | null>(null)
const alert = ref<PageAlert | null>(null)
const setup = ref<ResellerDomainSetupData | null>(null)
const checkingDomain = ref<number | null>(null)
const domainSetupSection = ref<HTMLElement | null>(null)
const showSetup = async (domain: ResellerDomainData) => {
  if (checkingDomain.value !== null) return
  checkingDomain.value = domain.id
  alert.value = null
  try {
    const response = await resellerAPI.domainSetup(domain.id)
    setup.value = response.data.data || null
    if (setup.value?.domain && snapshot.value) {
      snapshot.value.domains = snapshot.value.domains.map((item) => item.id === domain.id ? setup.value!.domain : item)
    }
    await nextTick()
    domainSetupSection.value?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  } catch (err: any) { alert.value = { level: 'error', message: err?.message || t('personalCenter.common.saveFailed') } }
  finally { checkingDomain.value = null }
}

const domains = computed(() => snapshot.value?.domains || [])
const canSubmitDomain = computed(() => snapshot.value?.profile?.status === RESELLER_PROFILE_STATUS_ACTIVE)
const isActiveVerifiedDomain = isResellerDomainAccessible
const systemDomain = computed(() => domains.value.find((d) => d.type === RESELLER_DOMAIN_TYPE_SUBDOMAIN) || null)
const customDomains = computed(() => domains.value.filter((d) => d.type !== RESELLER_DOMAIN_TYPE_SUBDOMAIN))
const activeDomains = computed(() => domains.value.filter(isActiveVerifiedDomain))
const primaryDomain = computed(() => activeDomains.value.find((d) => d.is_primary) || null)
const pendingDomains = computed(() => domains.value.filter(isResellerDomainPendingConnection))

const load = async () => {
  loading.value = true
  alert.value = null
  try {
    const response = await resellerAPI.managementProfile()
    snapshot.value = response.data.data || null
  } catch (err: any) {
    alert.value = { level: 'error', message: err?.message || t('personalCenter.common.saveFailed') }
  } finally {
    loading.value = false
  }
}

const mergeDomains = (updates: ResellerDomainData[]) => {
  if (!snapshot.value) return
  const merged = new Map(snapshot.value.domains.map((item) => [item.id, item]))
  updates.forEach((item) => merged.set(item.id, item))
  snapshot.value.domains = [...merged.values()]
}

const providerLabel = (provider?: string) => ({
  cloudflare: 'Cloudflare',
  aliyun: t('resellerConsole.domains.connect.aliyun'),
  dnspod: t('resellerConsole.domains.connect.dnspod'),
})[provider || ''] || t('resellerConsole.domains.connect.manualProvider')

const connectionProgress = (domain: ResellerDomainData): { label: string; tone: ResellerBadgeTone } => {
  const state = getResellerDomainConnectionState(domain)
  if (state === 'active') return { label: t('resellerConsole.domains.connect.phases.active'), tone: 'success' }
  if (state === 'approved_pending') return { label: t('resellerConsole.domains.approvedPending'), tone: 'warning' }
  if (state === 'disabled' || state === 'pending_review' || state === 'unknown') return { label: domainStatusLabel(domain.status), tone: domainTone(domain.status) }
  return { label: t(`resellerConsole.domains.connect.phases.${state}`), tone: 'warning' }
}

const domainStatusLabel = (status?: string) => t(`personalCenter.reseller.domainStatus.${getResellerDomainStatusKey(status)}`)

const domainTone = (status?: string): ResellerBadgeTone => {
  if (status === RESELLER_DOMAIN_STATUS_ACTIVE) return 'success'
  if (status === RESELLER_DOMAIN_STATUS_PENDING_REVIEW) return 'warning'
  if (status === RESELLER_DOMAIN_STATUS_DISABLED) return 'neutral'
  return 'neutral'
}

const verificationLabel = (status?: string) => {
  if (status === RESELLER_DOMAIN_VERIFICATION_VERIFIED) return t('personalCenter.reseller.domainVerification.verified')
  if (status === RESELLER_DOMAIN_VERIFICATION_PENDING) return t('personalCenter.reseller.domainVerification.pending')
  if (status === RESELLER_DOMAIN_VERIFICATION_FAILED) return t('personalCenter.reseller.domainVerification.failed')
  return t('personalCenter.reseller.domainVerification.unknown')
}

const verificationTone = (status?: string): ResellerBadgeTone => {
  if (status === RESELLER_DOMAIN_VERIFICATION_VERIFIED) return 'success'
  if (status === RESELLER_DOMAIN_VERIFICATION_PENDING) return 'warning'
  if (status === RESELLER_DOMAIN_VERIFICATION_FAILED) return 'warning'
  return 'neutral'
}

const domainTypeLabel = (type?: string) => {
  if (type === RESELLER_DOMAIN_TYPE_SUBDOMAIN) return t('personalCenter.reseller.domainType.subdomain')
  if (type === RESELLER_DOMAIN_TYPE_CUSTOM) return t('personalCenter.reseller.domainType.custom')
  return type || '-'
}

onMounted(load)
</script>
