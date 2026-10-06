<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
      <div class="min-w-0">
        <div class="truncate text-sm font-bold text-foreground">{{ label }}</div>
        <div v-if="!directPricing" class="mt-1 flex flex-wrap items-center gap-1.5">
          <Badge variant="neutral" size="xs">{{ t('personalCenter.reseller.productSettings.basePrice') }} {{ basePrice }}</Badge>
          <Badge :variant="invalid ? 'danger' : 'accent'" size="xs">{{ t('personalCenter.reseller.productSettings.effectivePrice') }} {{ effectivePrice || '-' }}</Badge>
          <span v-if="invalid && errorHint" class="text-xs font-medium text-destructive">{{ errorHint }}</span>
        </div>
      </div>
      <label class="inline-flex items-center gap-2 text-sm text-foreground">
        <Switch :model-value="modelValue.is_listed" :disabled="disabled" @update:model-value="updateBoolean" />
        {{ t('personalCenter.reseller.productSettings.listed') }}
      </label>
    </div>

    <div v-if="directPricing" class="grid grid-cols-1 gap-4 sm:grid-cols-2">
      <label class="space-y-2 text-sm text-muted-foreground">
        <span>{{ t('personalCenter.reseller.productSettings.supplyPrice') }}</span>
        <Input :model-value="supplyPrice || basePrice" readonly class="font-mono font-semibold bg-muted/50" />
      </label>
      <label class="space-y-2 text-sm text-muted-foreground">
        <span>{{ t('personalCenter.reseller.productSettings.myPrice') }}</span>
        <Input :model-value="sellingPrice" type="number" inputmode="decimal" step="0.01" :min="minPrice || 0.01" :disabled="disabled" :aria-invalid="Boolean(localPriceError || invalid)" class="font-mono" @update:model-value="updateSellingPrice" />
        <span class="block text-xs">{{ t('personalCenter.reseller.productSettings.minimumPrice') }} {{ minPrice || basePrice }}</span>
        <span v-if="localPriceError || (invalid && errorHint)" class="block text-xs text-destructive">{{ localPriceError || errorHint }}</span>
      </label>
      <div>
        <span class="text-sm text-muted-foreground">{{ t('personalCenter.reseller.productSettings.estimatedProfit') }}</span>
        <div class="mt-2 font-mono text-xl font-semibold" :class="Number(estimatedProfit) < 0 ? 'text-destructive' : 'text-success'">{{ estimatedProfit }}</div>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('personalCenter.reseller.productSettings.profitHint') }}</p>
      </div>
      <p v-if="disabled" class="text-xs text-muted-foreground">{{ t('personalCenter.reseller.productSettings.skuInactive') }}</p>
    </div>
    <div v-else class="grid grid-cols-1 gap-3 md:grid-cols-[220px_minmax(0,1fr)]">
      <Select :model-value="modelValue.pricing_mode" @update:model-value="(v) => updateString('pricing_mode', v)">
        <SelectTrigger>
          <SelectValue :placeholder="t('personalCenter.reseller.productSettings.pricingMode')" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="inherit">{{ t('personalCenter.reseller.productSettings.inherit') }}</SelectItem>
          <SelectItem value="markup_percent">{{ t('personalCenter.reseller.productSettings.markupPercent') }}</SelectItem>
          <SelectItem value="fixed_markup">{{ t('personalCenter.reseller.productSettings.fixedMarkup') }}</SelectItem>
          <SelectItem value="fixed_price">{{ t('personalCenter.reseller.productSettings.fixedPrice') }}</SelectItem>
        </SelectContent>
      </Select>
      <Input
        v-if="modelValue.pricing_mode === 'markup_percent'"
        :model-value="modelValue.markup_percent"
        type="text"
        inputmode="decimal"
        :placeholder="t('personalCenter.reseller.productSettings.markupPercentField')"
        @update:model-value="(v) => updateString('markup_percent', v)"
      />
      <Input
        v-else-if="modelValue.pricing_mode === 'fixed_markup'"
        :model-value="modelValue.fixed_markup_amount"
        type="text"
        inputmode="decimal"
        :placeholder="t('personalCenter.reseller.productSettings.fixedMarkupField')"
        @update:model-value="(v) => updateString('fixed_markup_amount', v)"
      />
      <Input
        v-else-if="modelValue.pricing_mode === 'fixed_price'"
        :model-value="modelValue.fixed_price_amount"
        type="text"
        inputmode="decimal"
        :placeholder="t('personalCenter.reseller.productSettings.fixedPriceField')"
        @update:model-value="(v) => updateString('fixed_price_amount', v)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { ResellerProductSettingPayloadItem } from '../../api/types'

const props = defineProps<{
  label: string
  basePrice: string
  effectivePrice?: string
  supplyPrice?: string
  minPrice?: string
  directPricing?: boolean
  disabled?: boolean
  invalid?: boolean
  errorCode?: string
  modelValue: ResellerProductSettingPayloadItem
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: ResellerProductSettingPayloadItem): void
}>()

const { t } = useI18n()

const errorHint = computed(() => {
  if (!props.invalid) return ''
  if (props.errorCode === 'markup_exceeded') return t('personalCenter.reseller.productSettings.markupExceededHint')
  return t('personalCenter.reseller.productSettings.priceInvalidHint')
})

const sellingPrice = computed(() => props.modelValue.pricing_mode === 'fixed_price'
  ? props.modelValue.fixed_price_amount
  : props.effectivePrice || '')
const localPriceError = computed(() => {
  const price = Number(sellingPrice.value)
  if (!sellingPrice.value || !Number.isFinite(price) || price <= 0) return t('personalCenter.reseller.productSettings.pricePositive')
  if (price < Number(props.minPrice || props.basePrice)) return t('personalCenter.reseller.productSettings.priceBelowMinimum')
  return ''
})
const estimatedProfit = computed(() => {
  const price = Number(sellingPrice.value)
  const supply = Number(props.supplyPrice || props.basePrice)
  return sellingPrice.value && Number.isFinite(price) && Number.isFinite(supply) ? (price - supply).toFixed(2) : '-'
})
const updateSellingPrice = (value: unknown) => emit('update:modelValue', {
  ...props.modelValue,
  pricing_mode: 'fixed_price',
  fixed_price_amount: String(value ?? '').trim(),
})

const updateValue = <K extends keyof ResellerProductSettingPayloadItem>(
  key: K,
  value: ResellerProductSettingPayloadItem[K],
) => {
  emit('update:modelValue', {
    ...props.modelValue,
    [key]: value,
  })
}

const updateString = (
  key: 'pricing_mode' | 'markup_percent' | 'fixed_markup_amount' | 'fixed_price_amount',
  value: unknown,
) => {
  updateValue(key, String(value ?? '').trim())
}

const updateBoolean = (value: boolean) => {
  updateValue('is_listed', value)
}
</script>
