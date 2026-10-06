<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <section class="overflow-hidden rounded-xl border bg-card">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b p-4">
      <h3 class="min-w-0 font-semibold">{{ title }}</h3>
      <label class="flex items-center gap-2 text-sm">
        <Switch :model-value="detail.product_setting?.is_listed !== false" :disabled="busy || !detail.product.is_active" @update:model-value="setProductListed" />{{ tr('listed') }}
      </label>
    </div>
    <p v-if="error" role="alert" class="px-4 pt-3 text-sm text-destructive">{{ error }}</p>
    <p v-if="success" role="status" class="px-4 pt-3 text-sm text-success">{{ success }}</p>
    <div class="hidden grid-cols-[minmax(120px,1fr)_minmax(110px,.8fr)_minmax(160px,1fr)_110px_100px_100px] gap-3 border-b bg-muted/30 px-4 py-3 text-xs font-medium text-muted-foreground xl:grid">
      <div>{{ tr('skuLevelRule') }}</div><div>{{ tr('supplyPrice') }}</div><div>{{ tr('myPrice') }}</div><div>{{ tr('estimatedProfit') }}</div><div>{{ tr('listed') }}</div><div></div>
    </div>
    <div v-for="sku in detail.skus" :key="sku.id" class="grid gap-4 border-b p-4 last:border-b-0 sm:grid-cols-2 xl:grid-cols-[minmax(120px,1fr)_minmax(110px,.8fr)_minmax(160px,1fr)_110px_100px_100px] xl:items-center xl:gap-3">
      <div class="text-sm font-medium sm:col-span-2 xl:col-span-1">{{ skuLabel(sku) }}<p v-if="!sku.is_active" class="mt-1 text-xs text-muted-foreground">{{ tr('skuInactive') }}</p></div>
      <div><label class="mb-2 block text-xs text-muted-foreground xl:hidden">{{ tr('supplyPrice') }}</label><div class="rounded-lg border bg-muted/30 p-3 font-mono text-sm">{{ formatPrice(sku.supply_price_amount ?? sku.base_price_amount) }}</div></div>
      <div>
        <label :for="`retail-${detail.product.id}-${sku.id}`" class="mb-2 block text-xs text-muted-foreground xl:sr-only">{{ tr('myPrice') }}</label>
        <Input :id="`retail-${detail.product.id}-${sku.id}`" :model-value="retail(sku)" type="number" inputmode="decimal" :min="minimum(sku)" step="0.01" class="font-mono" :disabled="busy || !sku.is_active || !detail.product.is_active" :aria-invalid="Boolean(priceError(sku))" :aria-describedby="`minimum-${detail.product.id}-${sku.id}`" @update:model-value="setRetail(sku.id, $event)" />
        <p :id="`minimum-${detail.product.id}-${sku.id}`" class="mt-1 text-xs text-muted-foreground">{{ tr('minimumPrice') }} {{ formatPrice(minimum(sku)) }}</p>
        <p v-if="priceError(sku)" class="mt-1 text-xs text-destructive">{{ tr(priceError(sku)) }}</p>
      </div>
      <div><p class="mb-1 text-xs text-muted-foreground xl:hidden">{{ tr('estimatedProfit') }}</p><p class="font-mono font-semibold" :class="priceError(sku) ? 'text-destructive' : 'text-success'">{{ formatPrice(resellerEstimatedProfit(retail(sku), sku.supply_price_amount ?? sku.base_price_amount)) }}</p></div>
      <label class="flex items-center gap-2 text-sm"><Switch :model-value="form(sku.id).is_listed" :disabled="busy || !sku.is_active || !detail.product.is_active" @update:model-value="form(sku.id).is_listed = $event" /><span class="xl:hidden">{{ tr('listed') }}</span></label>
      <Button size="sm" :disabled="busy || !changed(sku.id) || Boolean(priceError(sku)) || !sku.is_active || !detail.product.is_active" @click="save(sku)">{{ tr(busy ? 'saving' : 'save') }}</Button>
    </div>
    <p class="border-t px-4 py-3 text-xs text-muted-foreground">{{ tr('profitHint') }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLocalized } from '../../composables/useProduct'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { resellerAPI } from '../../api/reseller'
import type { ResellerProductSettingDetailData, ResellerProductSettingPayloadItem, ResellerProductSettingSKUData } from '../../api/types'
import { getLocalizedText } from '../../utils/resellerSiteConfig'
import { formatSkuSpecValues } from '../../utils/sku'
import { buildResellerProductSettingPayload, normalizeResellerProductSettingForm, resellerEstimatedProfit, resellerRetailPriceError } from '../../utils/resellerProductSettings'

const props = defineProps<{ detail: ResellerProductSettingDetailData }>()
const emit = defineEmits<{ (event: 'saved', detail: ResellerProductSettingDetailData): void; (event: 'dirty', value: boolean): void }>()
const { t, locale } = useI18n()
const { formatPrice } = useLocalized()
const tr = (key: string) => t(`personalCenter.reseller.productSettings.${key}`)
const title = computed(() => getLocalizedText(props.detail.product.title, String(locale.value)))
const forms = reactive<Record<number, ResellerProductSettingPayloadItem>>({})
const originals = reactive<Record<number, string>>({})
const busy = ref(false)
const error = ref('')
const success = ref('')
const form = (id: number) => forms[id] || normalizeResellerProductSettingForm({ sku_id: id })
const changed = (id: number) => JSON.stringify(form(id)) !== originals[id]
const dirty = computed(() => props.detail.skus.some((sku) => changed(sku.id)))
const applyDetail = (detail: ResellerProductSettingDetailData) => {
  for (const sku of detail.skus) {
    const pending = originals[sku.id] && changed(sku.id) ? { ...form(sku.id) } : null
    const saved = normalizeResellerProductSettingForm({ ...sku.setting, sku_id: sku.id })
    originals[sku.id] = JSON.stringify(saved)
    forms[sku.id] = pending || saved
  }
}
watch(() => props.detail, applyDetail, { immediate: true })
watch(dirty, (value) => emit('dirty', value))
const skuLabel = (sku: ResellerProductSettingSKUData) => formatSkuSpecValues(sku.spec_values, String(locale.value)) || sku.sku_code || `#${sku.id}`
const retail = (sku: ResellerProductSettingSKUData) => form(sku.id).pricing_mode === 'fixed_price' ? form(sku.id).fixed_price_amount : (sku.effective_price_amount || sku.setting?.effective_price_amount || '')
const minimum = (sku: ResellerProductSettingSKUData) => sku.min_price_amount ?? sku.supply_price_amount ?? sku.base_price_amount
const priceError = (sku: ResellerProductSettingSKUData) => resellerRetailPriceError(retail(sku), minimum(sku))
const setRetail = (id: number, value: unknown) => { forms[id] = { ...form(id), pricing_mode: 'fixed_price', fixed_price_amount: String(value ?? '').trim() } }
const save = async (sku: ResellerProductSettingSKUData) => {
  if (busy.value || priceError(sku) || !sku.is_active || !props.detail.product.is_active) return
  await update([form(sku.id)])
}
const setProductListed = async (listed: boolean) => {
  if (busy.value || !props.detail.product.is_active) return
  await update([{ ...normalizeResellerProductSettingForm({ ...props.detail.product_setting, sku_id: 0 }), is_listed: listed }])
}
const update = async (settings: ResellerProductSettingPayloadItem[]) => {
  busy.value = true; error.value = ''; success.value = ''
  try {
    const response = await resellerAPI.updateProductSettings(props.detail.product.id, buildResellerProductSettingPayload(settings))
    const detail = response.data?.data as ResellerProductSettingDetailData
    if (!detail?.product || !Array.isArray(detail.skus)) throw new Error(tr('saveFailed'))
    // Keep edits on other SKUs when saving one row.
    settings.forEach((item) => delete originals[item.sku_id])
    applyDetail(detail)
    emit('saved', detail)
    success.value = tr('saveSuccess')
  } catch (err: any) { error.value = err?.message || tr('saveFailed') }
  finally { busy.value = false }
}
</script>
