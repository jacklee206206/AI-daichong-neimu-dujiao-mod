<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <div class="space-y-5">
    <div class="flex flex-wrap gap-2 rounded-xl border bg-card p-2" role="tablist" :aria-label="tr('title')">
      <Button id="product-images-tab" role="tab" aria-controls="reseller-products-panel" :aria-selected="tab === 'images'" :variant="tab === 'images' ? 'default' : 'ghost'" @click="selectTab('images')"><ImageIcon class="h-4 w-4" />{{ tr('imagesTab') }}</Button>
      <Button id="product-pricing-tab" role="tab" aria-controls="reseller-products-panel" :aria-selected="tab === 'pricing'" :variant="tab === 'pricing' ? 'default' : 'ghost'" @click="selectTab('pricing')"><SlidersHorizontal class="h-4 w-4" />{{ tr('pricingTab') }}</Button>
    </div>
    <slot v-if="tab === 'pricing'" name="pricing-summary" />
    <p v-if="tab === 'images'" class="rounded-lg border bg-muted/30 p-4 text-sm text-muted-foreground">{{ tr('imageIntro') }}</p>
    <Card :class="embedded ? 'p-4' : 'p-6'">
      <div v-if="!embedded" class="mb-4"><h2 class="text-xl font-bold">{{ tr('title') }}</h2><p class="mt-1 text-sm text-muted-foreground">{{ tr('subtitle') }}</p></div>
      <div class="flex flex-wrap gap-3">
        <Input v-model.trim="keyword" class="min-w-48 flex-1" :placeholder="tr(tab === 'images' ? 'imagesSearchPlaceholder' : 'searchPlaceholder')" :aria-label="tr(tab === 'images' ? 'imagesSearchPlaceholder' : 'searchPlaceholder')" @keyup.enter="loadRows(1)" />
        <Button :disabled="loading" @click="loadRows(1)">{{ t('orders.filters.search') }}</Button>
        <Button variant="outline" :disabled="loading" @click="loadRows(pagination.page)"><RotateCw class="h-4 w-4" />{{ t('orders.filters.refresh') }}</Button>
      </div>
    </Card>
    <Alert v-if="error" variant="destructive"><AlertDescription>{{ error }}</AlertDescription></Alert>
    <div v-if="loading" class="space-y-3"><div v-for="i in 3" :key="i" class="h-36 animate-pulse rounded-xl border bg-muted"></div></div>
    <div v-else-if="rows.length === 0" class="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">{{ tr('empty') }}</div>
    <div v-else id="reseller-products-panel" role="tabpanel" :aria-labelledby="`product-${tab}-tab`" class="space-y-4">
      <template v-for="row in rows" :key="row.product.id">
        <ResellerProductPricing v-show="tab === 'pricing'" :detail="row" @saved="replaceRow" @dirty="dirtyStates[`price-${row.product.id}`] = $event" />
        <ResellerProductImages v-show="tab === 'images'" :detail="row" @saved="replaceRow" @dirty="dirtyStates[`image-${row.product.id}`] = $event" />
      </template>
    </div>
    <div v-if="pagination.total_page > 1" class="flex items-center justify-center gap-3">
      <Button variant="outline" size="sm" :disabled="loading || pagination.page <= 1" @click="loadRows(pagination.page - 1)">{{ t('orders.prevPage') }}</Button>
      <span class="text-sm text-muted-foreground">{{ t('orders.pageInfo', { page: pagination.page, total: pagination.total_page }) }}</span>
      <Button variant="outline" size="sm" :disabled="loading || pagination.page >= pagination.total_page" @click="loadRows(pagination.page + 1)">{{ t('orders.nextPage') }}</Button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ImageIcon, RotateCw, SlidersHorizontal } from 'lucide-vue-next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import ResellerProductImages from '../../components/reseller/ResellerProductImages.vue'
import ResellerProductPricing from '../../components/reseller/ResellerProductPricing.vue'
import { resellerAPI } from '../../api/reseller'
import type { ResellerProductSettingDetailData } from '../../api/types'
import { normalizeResellerProductSettingsPagination } from '../../utils/resellerProductSettings'

withDefaults(defineProps<{ embedded?: boolean }>(), { embedded: false })
const { t } = useI18n()
const tr = (key: string) => t(`personalCenter.reseller.productSettings.${key}`)
const route = useRoute()
const router = useRouter()
const tab = computed(() => route.query.tab === 'pricing' ? 'pricing' : 'images')
const selectTab = (value: 'images' | 'pricing') => void router.replace({ query: { ...route.query, tab: value } })
const keyword = ref('')
const loading = ref(false)
const error = ref('')
const rows = ref<ResellerProductSettingDetailData[]>([])
const pagination = reactive({ page: 1, page_size: 20, total: 0, total_page: 1 })
const dirtyStates = reactive<Record<string, boolean>>({})
const dirty = computed(() => Object.values(dirtyStates).some(Boolean))
const canDiscard = () => !dirty.value || window.confirm(tr('discardChanges'))
const clearDirty = () => Object.keys(dirtyStates).forEach((key) => delete dirtyStates[key])
const replaceRow = (detail: ResellerProductSettingDetailData) => {
  const index = rows.value.findIndex((row) => row.product.id === detail.product.id)
  if (index >= 0) rows.value[index] = detail
}
const loadRows = async (page = pagination.page) => {
  if (loading.value || !canDiscard()) return
  loading.value = true; error.value = ''
  try {
    const response = await resellerAPI.productSettings({ page, page_size: pagination.page_size, keyword: keyword.value || undefined })
    rows.value = response.data?.data || []
    Object.assign(pagination, normalizeResellerProductSettingsPagination(response.data?.pagination, pagination))
    clearDirty()
  } catch (err: any) { error.value = err?.message || tr('loadFailed') }
  finally { loading.value = false }
}
const beforeUnload = (event: BeforeUnloadEvent) => {
  if (!dirty.value) return
  event.preventDefault()
  event.returnValue = ''
}
onBeforeRouteLeave(canDiscard)
onMounted(() => { window.addEventListener('beforeunload', beforeUnload); void loadRows(1) })
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
</script>
