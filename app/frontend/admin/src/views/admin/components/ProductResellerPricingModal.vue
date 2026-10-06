<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminProduct, AdminProductSKU } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogScrollContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { getLocalizedText, formatMoney } from '@/utils/format'
import { notifySuccess } from '@/utils/notify'

const props = defineProps<{ modelValue: boolean; productId: number | null; siteCurrency: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; success: [] }>()
const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const product = ref<AdminProduct | null>(null)
type PriceRow = { sku: AdminProductSKU; supply: string; minimum: string }
const rows = ref<PriceRow[]>([])
let requestId = 0
const money = (value: number) => formatMoney(value.toFixed(2), props.siteCurrency)
const amountValid = (value: string | number) => /^\d+(\.\d{1,2})?$/.test(String(value).trim()) && Number.isFinite(Number(value))
const valid = computed(() => rows.value.length > 0 && rows.value.every(row => amountValid(row.supply) && amountValid(row.minimum)))
const effectiveSupply = (row: PriceRow) => Number(row.supply) > 0 ? Number(row.supply) : Number(row.sku.price_amount)
const effectiveMinimum = (row: PriceRow) => Math.max(effectiveSupply(row), Number(row.minimum) || 0)
const skuName = (sku: AdminProductSKU) => getLocalizedText(sku.spec_values) || sku.sku_code || `#${sku.id}`

watch(() => [props.modelValue, props.productId] as const, async ([open, id]) => {
  const request = ++requestId
  error.value = ''
  product.value = null
  rows.value = []
  if (!open || !id) return
  loading.value = true
  try {
    const response = await adminAPI.getProduct(id)
    if (request !== requestId) return
    product.value = response.data.data as AdminProduct
    rows.value = (product.value.skus || []).map(sku => ({
      sku,
      supply: Number(sku.reseller_supply_price_amount || 0).toFixed(2),
      minimum: Number(sku.reseller_min_price_amount || 0).toFixed(2),
    }))
  } catch (err: any) {
    if (request === requestId) error.value = err?.message || t('admin.products.resellerPricing.loadFailed')
  } finally {
    if (request === requestId) loading.value = false
  }
}, { immediate: true })

const save = async () => {
  if (!props.productId || !valid.value || saving.value || loading.value) return
  saving.value = true
  error.value = ''
  try {
    await adminAPI.updateProductResellerPrices(props.productId, {
      skus: rows.value.map(row => ({
        id: row.sku.id,
        reseller_supply_price_amount: Number(row.supply).toFixed(2),
        reseller_min_price_amount: Number(row.minimum).toFixed(2),
      })),
    })
    notifySuccess(t('admin.products.resellerPricing.saved'))
    emit('success')
    emit('update:modelValue', false)
  } catch (err: any) {
    error.value = err?.message || t('admin.products.resellerPricing.saveFailed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Dialog :open="modelValue" @update:open="value => { if (!saving) emit('update:modelValue', value) }">
    <DialogScrollContent class="max-w-3xl" @interact-outside="event => event.preventDefault()" @escape-key-down="event => { if (saving) event.preventDefault() }">
      <DialogHeader>
        <DialogTitle>{{ t('admin.products.resellerPricing.title') }}</DialogTitle>
      </DialogHeader>
      <p v-if="loading" class="py-8 text-center text-muted-foreground">{{ t('admin.products.resellerPricing.loading') }}</p>
      <template v-else-if="product">
        <div>
          <h2 class="font-semibold">{{ getLocalizedText(product.title) }}</h2>
          <p class="mt-2 text-sm text-muted-foreground">{{ t('admin.products.resellerPricing.scope') }}</p>
        </div>
        <div class="rounded-lg border border-primary/20 bg-primary/5 p-3 text-sm">
          {{ t('admin.products.resellerPricing.example') }}
        </div>
        <div v-for="row in rows" :key="row.sku.id" class="space-y-3 rounded-xl border border-border p-4">
          <div class="flex flex-wrap justify-between gap-2">
            <h3 class="font-medium">{{ skuName(row.sku) }}</h3>
            <span class="text-xs text-muted-foreground">{{ t('admin.products.resellerPricing.mainPrice') }} {{ money(Number(row.sku.price_amount)) }}</span>
          </div>
          <div class="grid gap-4 sm:grid-cols-2">
            <div>
              <Label :for="`reseller-supply-${row.sku.id}`">{{ t('admin.products.form.resellerSupplyPrice') }}</Label>
              <Input :id="`reseller-supply-${row.sku.id}`" v-model="row.supply" type="number" min="0" step="0.01" :disabled="saving" class="mt-2 font-mono" />
              <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.products.resellerPricing.supplyHint') }}</p>
            </div>
            <div>
              <Label :for="`reseller-minimum-${row.sku.id}`">{{ t('admin.products.form.resellerMinPrice') }}</Label>
              <Input :id="`reseller-minimum-${row.sku.id}`" v-model="row.minimum" type="number" min="0" step="0.01" :disabled="saving" class="mt-2 font-mono" />
              <p class="mt-1 text-xs text-muted-foreground">{{ t('admin.products.resellerPricing.minimumHint') }}</p>
            </div>
          </div>
          <div v-if="amountValid(row.supply) && amountValid(row.minimum)" class="flex flex-wrap gap-x-6 gap-y-2 rounded-lg bg-muted/40 p-3 text-sm">
            <span>{{ t('admin.products.resellerPricing.effectiveSupply') }} <strong>{{ money(effectiveSupply(row)) }}</strong></span>
            <span>{{ t('admin.products.resellerPricing.effectiveMinimum') }} <strong>{{ money(effectiveMinimum(row)) }}</strong></span>
            <span>{{ t('admin.products.resellerPricing.minimumProfit') }} <strong class="text-emerald-600">{{ money(effectiveMinimum(row) - effectiveSupply(row)) }}</strong></span>
          </div>
          <p v-else class="text-sm text-destructive">{{ t('admin.products.resellerPricing.invalid') }}</p>
        </div>
        <p v-if="!rows.length" class="text-sm text-muted-foreground">{{ t('admin.products.resellerPricing.empty') }}</p>
        <p class="text-xs leading-relaxed text-muted-foreground">{{ t('admin.products.resellerPricing.enforcement') }}</p>
      </template>
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
      <div class="flex justify-end gap-2">
        <Button variant="outline" :disabled="saving" @click="emit('update:modelValue', false)">{{ t('admin.common.cancel') }}</Button>
        <Button :disabled="saving || loading || !valid" @click="save">{{ saving ? t('admin.products.actions.submitting') : t('admin.products.resellerPricing.save') }}</Button>
      </div>
    </DialogScrollContent>
  </Dialog>
</template>
