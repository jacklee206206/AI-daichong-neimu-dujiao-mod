<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <section class="grid items-start gap-5 px-5 py-6 sm:gap-6 sm:px-7 sm:py-7 lg:grid-cols-[248px_minmax(0,1fr)]">
    <div class="flex items-start gap-3.5">
      <span class="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary"><Zap class="h-5 w-5" /></span>
      <div class="min-w-0 pt-0.5">
        <ResellerStatusBadge :label="t('resellerConsole.domains.connect.step')" tone="accent" class="rounded-full px-2.5 py-0.5" />
        <h3 class="mt-2 text-base font-bold leading-6 text-foreground">{{ t('resellerConsole.domains.connect.title') }}</h3>
        <p class="mt-2 max-w-[172px] text-sm leading-6 text-muted-foreground">{{ t('resellerConsole.domains.connect.rootHint') }}</p>
      </div>
    </div>
    <div class="min-w-0 space-y-4">
      <div class="rounded-xl border border-border bg-muted/40 p-4 sm:p-5">
        <div class="mb-4 flex flex-wrap items-center gap-x-2 gap-y-2 text-sm">
          <span class="mr-1 text-muted-foreground">{{ t('resellerConsole.domains.connect.supported') }}</span>
          <span v-for="provider in providerLabels" :key="provider" class="rounded-full border border-border bg-muted/50 px-2.5 py-0.5 text-xs font-medium leading-5 text-muted-foreground">{{ provider }}</span>
        </div>
        <form v-if="enabled" class="flex flex-col gap-3 xl:flex-row" @submit.prevent="inspectDomain">
          <label class="sr-only" for="reseller-root-domain">{{ t('resellerConsole.domains.connect.rootDomain') }}</label>
          <Input id="reseller-root-domain" v-model.trim="domain" type="text" inputmode="url" autocomplete="off" autocapitalize="none" spellcheck="false" class="h-11 flex-1 rounded-xl bg-background/40 px-3.5 text-sm shadow-none" :disabled="busy" :placeholder="t('resellerConsole.domains.connect.placeholder')" />
          <Button type="submit" class="h-11 shrink-0 rounded-xl px-5 text-sm font-medium shadow-none xl:min-w-[190px]" :disabled="busy || !domain.trim()"><LoaderCircle v-if="inspecting" class="h-4 w-4 animate-spin" /><Zap v-else class="h-4 w-4" />{{ inspecting ? t('resellerConsole.domains.connect.inspecting') : t('resellerConsole.domains.connect.inspect') }}</Button>
        </form>
        <p v-else class="rounded-lg border border-dashed border-border bg-background p-3 text-sm text-muted-foreground">{{ t('resellerConsole.domains.submitDisabledDesc') }}</p>
        <p class="mt-3 text-xs leading-6 text-muted-foreground">{{ t('resellerConsole.domains.connect.conflictHint') }}</p>
      </div>

      <Alert v-if="error" variant="destructive"><AlertDescription>{{ error }}</AlertDescription></Alert>

      <section v-if="inspection && !connection" class="rounded-xl border border-border bg-background p-4 sm:p-5" aria-live="polite">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div><h4 class="font-semibold">{{ t('resellerConsole.domains.connect.detected') }}</h4><p class="mt-1 text-sm text-muted-foreground">{{ inspection.root_domain }} · {{ inspection.provider_label || t('resellerConsole.domains.connect.unknownProvider') }}</p></div>
          <Button variant="ghost" size="sm" :disabled="connecting" @click="closeInspection"><X class="h-4 w-4" /><span class="sr-only">{{ t('resellerConsole.domains.connect.close') }}</span></Button>
        </div>
        <Alert v-if="inspection.conflicts?.length" variant="destructive" class="mt-4">
          <AlertDescription><p class="font-semibold">{{ t('resellerConsole.domains.connect.stopped') }}</p><ul class="mt-2 list-disc space-y-1 pl-4"><li v-for="(conflict, index) in inspection.conflicts" :key="index">{{ conflict }}</li></ul></AlertDescription>
        </Alert>
        <section v-if="saasMode && inspection.can_restart" class="mt-4 space-y-4 rounded-lg border border-border bg-muted/20 p-4">
          <div><h5 class="text-sm font-semibold">{{ t('resellerConsole.domains.connect.restartTitle') }}</h5><p class="mt-2 text-sm leading-relaxed text-muted-foreground">{{ inspection.restart_message || t('resellerConsole.domains.connect.restartHint') }}</p></div>
          <Alert v-if="!inspection.platform_ready"><AlertDescription>{{ inspection.platform_message || t('resellerConsole.domains.connect.platformPending') }}</AlertDescription></Alert>
          <label class="flex items-start gap-3 text-sm leading-relaxed">
            <input v-model="restartConfirmed" type="checkbox" class="mt-1 h-4 w-4 shrink-0 accent-primary" :disabled="busy || !inspection.platform_ready" />
            <span>{{ t('resellerConsole.domains.connect.restartConfirm') }}</span>
          </label>
          <Button class="h-auto min-h-9 whitespace-normal text-left" :disabled="!enabled || busy || !canRestartDomainConnection(inspection, restartConfirmed)" @click="restartConnection"><LoaderCircle v-if="connecting" class="h-4 w-4 animate-spin" /><RotateCw v-else class="h-4 w-4" />{{ connecting ? t('resellerConsole.domains.connect.restarting') : t('resellerConsole.domains.connect.restart') }}</Button>
        </section>
        <template v-if="!inspection.conflicts?.length && !inspection.can_restart">
          <div class="mt-4 rounded-lg border border-border bg-muted/20 p-3 text-sm">
            <p class="font-medium">{{ t('resellerConsole.domains.connect.recordsTitle') }}</p>
            <div class="mt-2 space-y-1 font-mono text-xs"><p v-for="host in inspection.domains" :key="host" class="break-all">{{ host }} → {{ saasMode ? 'CNAME' : 'A' }} {{ saasMode ? inspection.entry_host : inspection.entry_ip }}</p></div>
            <p class="mt-2 text-xs leading-relaxed text-muted-foreground">{{ t(`resellerConsole.domains.connect.${saasMode ? 'saasRecordsHint' : 'recordsHint'}`) }}</p>
          </div>
          <Alert v-if="saasMode && !inspection.platform_ready" class="mt-4"><AlertDescription>{{ inspection.platform_message || t('resellerConsole.domains.connect.platformPending') }}</AlertDescription></Alert>
          <div v-if="saasMode" class="mt-4 space-y-3">
            <p class="text-sm text-muted-foreground">{{ t('resellerConsole.domains.connect.saasManualHint') }}</p>
            <Button :disabled="busy || !canStartManualConnection(inspection)" @click="startManualSetup"><LoaderCircle v-if="connecting" class="h-4 w-4 animate-spin" />{{ t('resellerConsole.domains.connect.saasManual') }}</Button>
          </div>
          <form v-if="canAuthorize" class="mt-4 space-y-4" autocomplete="off" @submit.prevent="authorizeConnection">
            <div><h5 class="text-sm font-semibold">{{ t('resellerConsole.domains.connect.authorization') }}</h5><p class="mt-1 text-xs leading-relaxed text-muted-foreground">{{ t('resellerConsole.domains.connect.authorizationHint') }}</p></div>
            <ul v-if="inspection.required_permissions?.length" class="list-disc space-y-1 pl-4 text-xs text-muted-foreground"><li v-for="permission in inspection.required_permissions" :key="permission">{{ permission }}</li></ul>
            <div class="grid gap-3 sm:grid-cols-2">
              <div v-for="field in credentialFields" :key="field" :class="credentialFields.length === 1 ? 'sm:col-span-2' : ''">
                <label :for="`dns-${field}`" class="mb-1.5 block text-sm font-medium">{{ credentialLabels[field] }}</label>
                <Input :id="`dns-${field}`" v-model="credentials[field]" type="password" autocomplete="new-password" autocapitalize="none" spellcheck="false" :disabled="connecting" required />
              </div>
            </div>
            <p class="text-xs leading-relaxed text-muted-foreground">{{ t('resellerConsole.domains.connect.credentialsHint') }}</p>
            <Button type="submit" :disabled="connecting || !credentialsReady"><LoaderCircle v-if="connecting" class="h-4 w-4 animate-spin" /><ShieldCheck v-else class="h-4 w-4" />{{ connecting ? t('resellerConsole.domains.connect.connecting') : t('resellerConsole.domains.connect.authorize') }}</Button>
          </form>
          <div v-else-if="!saasMode" class="mt-4 space-y-3">
            <p class="text-sm text-muted-foreground">{{ t('resellerConsole.domains.connect.manualHint') }}</p>
            <Button variant="outline" :disabled="connecting" @click="startManualSetup"><LoaderCircle v-if="connecting" class="h-4 w-4 animate-spin" />{{ t('resellerConsole.domains.connect.manual') }}</Button>
          </div>
          <ul v-if="inspection.instructions?.length" class="mt-4 list-disc space-y-1 pl-4 text-xs leading-relaxed text-muted-foreground"><li v-for="line in inspection.instructions" :key="line">{{ line }}</li></ul>
        </template>
      </section>

      <section v-if="connection" class="rounded-xl border border-border bg-background p-4 sm:p-5" aria-live="polite">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h4 class="flex items-center gap-2 font-semibold"><CheckCircle2 v-if="connected" class="h-5 w-5 text-success" /><LoaderCircle v-else-if="polling || connecting" class="h-5 w-5 animate-spin text-primary" /><CircleAlert v-else class="h-5 w-5 text-warning" />{{ phaseLabel }}</h4>
          <Button v-if="!connected" variant="outline" size="sm" :disabled="busy" @click="refreshConnection"><RotateCw class="h-4 w-4" />{{ t('orders.filters.refresh') }}</Button>
        </div>
        <p v-if="connection.message" class="mt-3 text-sm leading-relaxed text-muted-foreground">{{ connection.message }}</p>
        <div v-if="connectionSetups.length && !connected" class="mt-4 space-y-5">
          <section v-for="item in connectionSetups" :key="item.domain.id" class="rounded-lg border border-border p-3">
            <h5 class="break-all font-mono text-sm font-semibold">{{ item.domain.domain }}</h5>
            <div v-for="record in item.records" :key="`${record.type}:${record.name}:${record.value}`" class="mt-3 space-y-1 rounded-lg bg-muted/30 p-3 text-xs">
              <p class="font-semibold">{{ record.type }}</p>
              <div class="flex items-start justify-between gap-2"><span class="break-all font-mono">{{ record.name }}</span><ResellerCopyButton :value="record.name" /></div>
              <div class="flex items-start justify-between gap-2"><span class="break-all font-mono">{{ record.value }}</span><ResellerCopyButton :value="record.value" /></div>
            </div>
            <ul class="mt-3 list-disc space-y-1 pl-4 text-xs leading-relaxed text-muted-foreground"><li v-for="line in item.instructions" :key="line">{{ line }}</li></ul>
          </section>
        </div>
        <p v-if="polling" class="mt-2 text-xs text-muted-foreground">{{ t('resellerConsole.domains.connect.polling') }}</p>
        <p v-else-if="domainConnectionShouldPoll(connection)" class="mt-2 text-xs text-muted-foreground">{{ t('resellerConsole.domains.connect.pendingHint') }}</p>
        <Button v-if="connection.phase === 'failed' || connection.phase === 'manual_required'" class="mt-4" variant="outline" :disabled="busy" @click="inspectDomain">{{ t('resellerConsole.domains.connect.retry') }}</Button>
      </section>

      <details v-if="enabled && (inspection || connection || error)" class="rounded-xl border border-border bg-background p-4 sm:p-5">
        <summary class="cursor-pointer text-sm font-semibold">{{ t('resellerConsole.domains.connect.manualAddTitle') }}</summary>
        <p class="mt-3 text-xs leading-relaxed text-muted-foreground">{{ t('resellerConsole.domains.connect.manualAddHint') }}</p>
        <form class="mt-3 flex flex-col gap-3 xl:flex-row" @submit.prevent="submitManualDomain">
          <label class="sr-only" for="reseller-manual-domain">{{ t('resellerConsole.domains.connect.manualDomain') }}</label>
          <Input id="reseller-manual-domain" v-model.trim="manualDomain" type="text" inputmode="url" autocomplete="off" autocapitalize="none" spellcheck="false" :disabled="busy" :placeholder="t('resellerConsole.domains.connect.manualPlaceholder')" />
          <Button type="submit" variant="outline" class="shrink-0" :disabled="busy || !manualDomain.trim()"><LoaderCircle v-if="connecting" class="h-4 w-4 animate-spin" />{{ t('resellerConsole.domains.connect.manualAdd') }}</Button>
        </form>
      </details>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CheckCircle2, CircleAlert, LoaderCircle, RotateCw, ShieldCheck, X, Zap } from 'lucide-vue-next'
import { resellerAPI, type ResellerDomainConnectInspection, type ResellerDomainConnectResult, type ResellerDomainData, type ResellerDomainSetupData } from '../../api'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import ResellerStatusBadge from '../reseller-console/ResellerStatusBadge.vue'
import ResellerCopyButton from '../reseller-console/ResellerCopyButton.vue'
import { canAuthorizeDomainConnection, canRestartDomainConnection, canStartManualConnection, clearDomainCredentials, domainConnectionComplete, domainConnectionShouldPoll, domainCredentialFields, emptyDomainCredentials, takeDomainCredentials, validDomainConnectInput, validManualDomainInput, type DomainCredentialField } from '../../utils/resellerDomainConnect'

const props = defineProps<{ enabled: boolean; domains: ResellerDomainData[] }>()
const emit = defineEmits<{ domains: [domains: ResellerDomainData[]]; manual: [domain: ResellerDomainData] }>()
const { t } = useI18n()
const providerLabels = computed(() => ['Cloudflare', t('resellerConsole.domains.connect.aliyun'), t('resellerConsole.domains.connect.dnspod')])
const credentialLabels: Record<DomainCredentialField, string> = { api_token: 'API Token', access_key_id: 'AccessKey ID', access_key_secret: 'AccessKey Secret', secret_id: 'SecretId', secret_key: 'SecretKey' }
const domain = ref('')
const manualDomain = ref('')
const error = ref('')
const restartConfirmed = ref(false)
const inspecting = ref(false)
const connecting = ref(false)
const polling = ref(false)
const inspection = ref<ResellerDomainConnectInspection | null>(null)
const connection = ref<ResellerDomainConnectResult | null>(null)
const connectionSetups = ref<ResellerDomainSetupData[]>([])
const saasMode = computed(() => inspection.value?.connect_mode === 'cloudflare_saas')
const credentials = reactive(emptyDomainCredentials())
const busy = computed(() => inspecting.value || connecting.value)
const canAuthorize = computed(() => canAuthorizeDomainConnection(inspection.value))
const credentialFields = computed(() => domainCredentialFields[inspection.value?.provider || 'unsupported'] || [])
const credentialsReady = computed(() => credentialFields.value.length > 0 && credentialFields.value.every((key) => credentials[key].trim()))
const connected = computed(() => domainConnectionComplete(connection.value))
const phaseLabel = computed(() => t(`resellerConsole.domains.connect.phases.${connection.value?.phase === 'active' && !connected.value ? 'tls_pending' : connection.value?.phase || 'waiting_authorization'}`))
let pollTimer: ReturnType<typeof setTimeout> | undefined
let operation = 0
let disposed = false

const stopPolling = () => { if (pollTimer) clearTimeout(pollTimer); pollTimer = undefined; polling.value = false }
const closeInspection = () => { clearDomainCredentials(credentials); inspection.value = null; restartConfirmed.value = false }
const messageForError = (err: unknown) => err instanceof Error && err.message ? err.message : t('personalCenter.common.saveFailed')
const acceptConnection = async (result: ResellerDomainConnectResult, request: number) => {
  connection.value = result
  emit('domains', result.domains || [])
  if (saasMode.value && !domainConnectionComplete(result)) {
    const responses = await Promise.all((result.domains || []).map(row => resellerAPI.domainSetup(row.id)))
    if (!disposed && request === operation) connectionSetups.value = responses.map(response => response.data.data)
  } else connectionSetups.value = []
}

watch(domain, () => { operation++; stopPolling(); closeInspection(); connection.value = null; connectionSetups.value = []; error.value = '' })

async function inspectDomain() {
  if (!props.enabled || busy.value) return
  const normalized = domain.value.trim().toLowerCase()
  if (!validDomainConnectInput(normalized)) { error.value = t('resellerConsole.domains.connect.invalidDomain'); return }
  stopPolling()
  clearDomainCredentials(credentials)
  inspection.value = null
  restartConfirmed.value = false
  connection.value = null
  connectionSetups.value = []
  error.value = ''
  inspecting.value = true
  const request = ++operation
  try {
    const response = await resellerAPI.inspectDomainConnection(normalized)
    if (disposed || request !== operation) return
    inspection.value = response.data.data
  } catch (err) { if (!disposed && request === operation) error.value = messageForError(err) }
  finally { if (!disposed && request === operation) inspecting.value = false }
}

async function pollConnection(root: string, request: number, remaining = 12) {
  if (disposed || request !== operation || remaining <= 0 || !domainConnectionShouldPoll(connection.value)) { polling.value = false; return }
  polling.value = true
  pollTimer = setTimeout(async () => {
    try {
      const response = await resellerAPI.domainConnectionStatus(root)
      if (disposed || request !== operation) return
      await acceptConnection(response.data.data, request)
      await pollConnection(root, request, remaining - 1)
    } catch (err) { if (!disposed && request === operation) { error.value = messageForError(err); polling.value = false } }
  }, 5000)
}

async function authorizeConnection() {
  if (!inspection.value || !canAuthorize.value || !credentialsReady.value || busy.value) return
  error.value = ''
  connecting.value = true
  const request = ++operation
  const root = inspection.value.root_domain
  const payload = { domain: root, provider: inspection.value.provider, credentials: takeDomainCredentials(inspection.value.provider, credentials) }
  try {
    const response = await resellerAPI.connectDomain(payload)
    if (disposed || request !== operation) return
    await acceptConnection(response.data.data, request)
    await pollConnection(root, request)
  } catch (err) { if (!disposed && request === operation) error.value = messageForError(err) }
  finally {
    for (const key of Object.keys(payload.credentials)) payload.credentials[key] = ''
    clearDomainCredentials(credentials)
    if (!disposed && request === operation) connecting.value = false
  }
}

async function restartConnection() {
  if (!props.enabled || !inspection.value || busy.value || !canRestartDomainConnection(inspection.value, restartConfirmed.value)) return
  stopPolling()
  clearDomainCredentials(credentials)
  error.value = ''
  connecting.value = true
  const request = ++operation
  const root = inspection.value.root_domain
  try {
    const response = await resellerAPI.restartDomainConnection(root, true)
    if (disposed || request !== operation) return
    restartConfirmed.value = false
    await acceptConnection(response.data.data, request)
    await pollConnection(root, request)
  } catch (err) { if (!disposed && request === operation) error.value = messageForError(err) }
  finally { if (!disposed && request === operation) connecting.value = false }
}

async function refreshConnection() {
  if (!inspection.value || busy.value) return
  stopPolling()
  error.value = ''
  const request = ++operation
  inspecting.value = true
  try {
    const response = await resellerAPI.domainConnectionStatus(inspection.value.root_domain)
    if (disposed || request !== operation) return
    await acceptConnection(response.data.data, request)
    await pollConnection(inspection.value.root_domain, request)
  } catch (err) { if (!disposed && request === operation) error.value = messageForError(err) }
  finally { if (!disposed && request === operation) inspecting.value = false }
}

async function startManualSetup() {
  if (!inspection.value || inspection.value.conflicts?.length || busy.value) return
  if (saasMode.value && !canStartManualConnection(inspection.value)) return
  connecting.value = true
  error.value = ''
  const request = ++operation
  const added: ResellerDomainData[] = []
  try {
    if (saasMode.value) {
      const root = inspection.value.root_domain
      const response = await resellerAPI.connectDomain({ domain: root, provider: 'manual', credentials: {} })
      if (disposed || request !== operation) return
      await acceptConnection(response.data.data, request)
      await pollConnection(root, request)
      return
    }
    for (const host of inspection.value.domains) {
      const existing = props.domains.find((item) => item.domain === host)
      const row = existing || (await resellerAPI.submitDomain({ domain: host })).data.data
      if (disposed || request !== operation) return
      added.push(row)
      emit('domains', [row])
    }
    if (added[0]) emit('manual', added[0])
  } catch (err) { if (!disposed && request === operation) error.value = messageForError(err) }
  finally { clearDomainCredentials(credentials); if (!disposed && request === operation) connecting.value = false }
}

async function submitManualDomain() {
  if (!props.enabled || busy.value) return
  const host = manualDomain.value.trim().toLowerCase()
  if (!validManualDomainInput(host)) { error.value = t('resellerConsole.domains.connect.manualInvalid'); return }
  stopPolling()
  closeInspection()
  connection.value = null
  error.value = ''
  connecting.value = true
  const request = ++operation
  try {
    const row = props.domains.find((item) => item.domain === host) || (await resellerAPI.submitDomain({ domain: host })).data.data
    if (disposed || request !== operation) return
    emit('domains', [row])
    emit('manual', row)
    manualDomain.value = ''
  } catch (err) { if (!disposed && request === operation) error.value = messageForError(err) }
  finally { if (!disposed && request === operation) connecting.value = false }
}

onBeforeUnmount(() => { disposed = true; operation++; stopPolling(); clearDomainCredentials(credentials) })
</script>
