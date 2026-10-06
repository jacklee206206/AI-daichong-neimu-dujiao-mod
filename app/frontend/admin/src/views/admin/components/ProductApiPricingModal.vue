<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAdminAuthStore } from '@/stores/auth'
import type { AdminProduct, AdminProductSKU } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogScrollContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { getLocalizedText, formatMoney } from '@/utils/format'
import { notifySuccess } from '@/utils/notify'

const props = defineProps<{ modelValue: boolean; productId: number | null; siteCurrency: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; success: [] }>()
const { t } = useI18n()
const auth = useAdminAuthStore()
const canManage = computed(() => auth.hasPermission('PUT:/admin/products/:id/api-prices'))
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const product = ref<AdminProduct | null>(null)
type PriceRow = { sku: AdminProductSKU; supply: string }
const rows = ref<PriceRow[]>([])
let requestId = 0

const amountValid = (value: string | number) => /^\d{1,18}(\.\d{1,2})?$/.test(String(value).trim())
const normalizeAmount = (value: string | number) => {
  const [whole = '0', fraction = ''] = String(value).trim().split('.')
  return `${whole.replace(/^0+(?=\d)/, '')}.${fraction.padEnd(2, '0')}`
}
const valid = computed(() => rows.value.length > 0 && rows.value.every(row => amountValid(row.supply)))
const skuName = (sku: AdminProductSKU) => getLocalizedText(sku.spec_values) || sku.sku_code || `#${sku.id}`

watch(() => [props.modelValue, props.productId] as const, async ([open, id]) => {
  const request = ++requestId
  error.value = ''
  product.value = null
  rows.value = []
  loading.value = false
  if (!open || !id) return
  loading.value = true
  try {
    const response = await adminAPI.getProduct(id)
    if (request !== requestId) return
    product.value = response.data.data as AdminProduct
    rows.value = (product.value.skus || []).map(sku => ({
      sku,
      supply: normalizeAmount(sku.api_supply_price_amount ?? 0),
    }))
  } catch (err: any) {
    if (request === requestId) error.value = err?.message || t('admin.products.apiPricing.loadFailed')
  } finally {
    if (request === requestId) loading.value = false
  }
}, { immediate: true })

const save = async () => {
  if (!props.productId || !canManage.value || !valid.value || saving.value || loading.value) return
  saving.value = true
  error.value = ''
  try {
    await adminAPI.updateProductApiPrices(props.productId, {
      skus: rows.value.map(row => ({ id: row.sku.id, api_supply_price_amount: normalizeAmount(row.supply) })),
    })
    notifySuccess(t('admin.products.apiPricing.saved'))
    emit('success')
    emit('update:modelValue', false)
  } catch (err: any) {
    error.value = err?.message || t('admin.products.apiPricing.saveFailed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Dialog :open="modelValue" @update:open="value => { if (!saving) emit('update:modelValue', value) }">
    <DialogScrollContent class="max-w-2xl" @interact-outside="event => event.preventDefault()" @escape-key-down="event => { if (saving) event.preventDefault() }">
      <DialogHeader>
        <DialogTitle>{{ t('admin.products.apiPricing.title') }}</DialogTitle>
        <DialogDescription>{{ t('admin.products.apiPricing.scope') }}</DialogDescription>
      </DialogHeader>
      <p v-if="loading" class="py-8 text-center text-muted-foreground">{{ t('admin.products.apiPricing.loading') }}</p>
      <template v-else-if="product">
        <h2 class="font-semibold">{{ getLocalizedText(product.title) }}</h2>
        <p id="api-pricing-hint" class="rounded-lg border border-primary/20 bg-primary/5 p-3 text-sm">{{ t('admin.products.apiPricing.hint') }}</p>
        <div v-if="rows.length" class="overflow-x-auto rounded-lg border border-border">
          <table class="w-full text-sm">
            <thead class="bg-muted/40 text-left">
              <tr>
                <th scope="col" class="px-3 py-3 font-medium">{{ t('admin.products.apiPricing.sku') }}</th>
                <th scope="col" class="px-3 py-3 font-medium">{{ t('admin.products.apiPricing.mainPrice') }}</th>
                <th scope="col" class="px-3 py-3 font-medium">{{ t('admin.products.apiPricing.title') }} ({{ siteCurrency }})</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in rows" :key="row.sku.id" class="border-t border-border">
                <th scope="row" class="px-3 py-3 text-left font-medium">{{ skuName(row.sku) }}</th>
                <td class="whitespace-nowrap px-3 py-3 text-muted-foreground">{{ formatMoney(row.sku.price_amount, siteCurrency) }}</td>
                <td class="min-w-40 px-3 py-3">
                  <Label :for="`api-supply-${row.sku.id}`" class="sr-only">{{ skuName(row.sku) }} {{ t('admin.products.apiPricing.title') }}</Label>
                  <Input
                    :id="`api-supply-${row.sku.id}`"
                    v-model="row.supply"
                    type="text"
                    inputmode="decimal"
                    :disabled="saving"
                    :aria-invalid="!amountValid(row.supply)"
                    :aria-describedby="amountValid(row.supply) ? 'api-pricing-hint' : `api-supply-error-${row.sku.id}`"
                    class="font-mono"
                  />
                  <p v-if="!amountValid(row.supply)" :id="`api-supply-error-${row.sku.id}`" class="mt-1 text-xs text-destructive">{{ t('admin.products.apiPricing.invalid') }}</p>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="text-sm text-muted-foreground">{{ t('admin.products.apiPricing.empty') }}</p>
      </template>
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
      <div class="flex justify-end gap-2">
        <Button variant="outline" :disabled="saving" @click="emit('update:modelValue', false)">{{ t('admin.common.cancel') }}</Button>
        <Button :disabled="saving || loading || !valid || !canManage" @click="save">{{ saving ? t('admin.products.actions.submitting') : t('admin.products.apiPricing.save') }}</Button>
      </div>
    </DialogScrollContent>
  </Dialog>
</template>
