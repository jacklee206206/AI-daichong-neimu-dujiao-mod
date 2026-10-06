<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAdminAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { notifySuccess } from '@/utils/notify'

const emit = defineEmits<{ (e: 'saved'): void }>()
const { t } = useI18n()
const auth = useAdminAuthStore()
const canRead = computed(() => auth.hasPermission('GET:/admin/settings'))
const canWrite = computed(() => auth.hasPermission('PUT:/admin/settings'))
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const error = ref('')
const autoApprove = ref(false)
const savedAutoApprove = ref(false)
const config = ref<Record<string, unknown>>({})

const loadConfig = async () => {
  if (!canRead.value) return
  loading.value = true
  loaded.value = false
  error.value = ''
  try {
    const response = await adminAPI.getSettings({ key: 'api_credential_config' })
    const data = response.data?.data
    config.value = data && typeof data === 'object' && !Array.isArray(data) ? data : {}
    autoApprove.value = config.value.auto_approve_applications === true
    savedAutoApprove.value = autoApprove.value
    loaded.value = true
  } catch (err: any) {
    error.value = err?.message || t('apiCredentials.reviewSettings.loadFailed')
  } finally {
    loading.value = false
  }
}

const save = async () => {
  if (!loaded.value || loading.value || saving.value || !canWrite.value) return
  saving.value = true
  error.value = ''
  const value = { ...config.value, auto_approve_applications: autoApprove.value }
  try {
    await adminAPI.updateSettings({ key: 'api_credential_config', value })
    config.value = value
    savedAutoApprove.value = value.auto_approve_applications
    notifySuccess(t('apiCredentials.reviewSettings.saved'))
    emit('saved')
  } catch (err: any) {
    // A failed response may follow a committed settings update. Reload before
    // claiming which mode is active or allowing another write.
    loaded.value = false
    error.value = err?.message || t('apiCredentials.reviewSettings.saveFailed')
  } finally {
    saving.value = false
  }
}

onMounted(loadConfig)
</script>

<template>
  <section v-if="canRead" class="rounded-xl border bg-card p-4 sm:p-5">
    <h2 class="font-semibold">{{ t('apiCredentials.reviewSettings.title') }}</h2>
    <p class="mt-1 text-sm text-muted-foreground">{{ t('apiCredentials.reviewSettings.description') }}</p>
    <p v-if="loaded" class="mt-3 text-sm font-medium" role="status">
      {{ t('apiCredentials.reviewSettings.currentMode') }}：{{ t(savedAutoApprove ? 'apiCredentials.reviewSettings.automatic' : 'apiCredentials.reviewSettings.manual') }}
    </p>
    <div class="mt-4 flex flex-wrap items-center gap-3">
      <Switch id="api-auto-approve" v-model="autoApprove" :disabled="!loaded || loading || saving || !canWrite" />
      <Label for="api-auto-approve">{{ t('apiCredentials.reviewSettings.automatic') }}</Label>
      <Button type="button" size="sm" :disabled="!loaded || loading || saving || !canWrite" @click="save">
        {{ t(saving ? 'apiCredentials.reviewSettings.saving' : 'apiCredentials.reviewSettings.save') }}
      </Button>
    </div>
    <p class="mt-2 text-xs text-muted-foreground">{{ t('apiCredentials.reviewSettings.scopeHint') }}</p>
    <p v-if="loaded && autoApprove !== savedAutoApprove" class="mt-2 text-xs text-amber-700">{{ t('apiCredentials.reviewSettings.unsaved') }}</p>
    <div v-if="error" role="alert" class="mt-3 text-sm text-destructive">
      {{ error }}
      <Button v-if="!loaded" type="button" size="sm" variant="outline" class="ml-2" :disabled="loading" @click="loadConfig">{{ t('apiCredentials.reviewSettings.retry') }}</Button>
    </div>
  </section>
</template>
