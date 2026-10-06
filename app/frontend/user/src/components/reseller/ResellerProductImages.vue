<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <section class="rounded-xl border bg-card p-4 sm:p-6" :aria-busy="busy">
    <div class="flex flex-col justify-between gap-3 sm:flex-row">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-3">
          <h3 class="font-semibold">{{ title }}</h3>
          <Badge :variant="detail.product.images_customized ? 'accent' : 'neutral'">{{ tr(detail.product.images_customized ? 'customImages' : 'platformImages') }}</Badge>
        </div>
        <p class="mt-1 text-sm text-muted-foreground">{{ tr('imageScope') }}</p>
      </div>
      <div class="flex flex-wrap items-start gap-2">
        <Button size="sm" :disabled="busy || !detail.product.is_active || (!inherited && images.length >= 20)" @click="fileInput?.click()"><ImagePlus class="h-4 w-4" />{{ tr('uploadImages') }}</Button>
        <Button size="sm" variant="outline" :disabled="busy || !detail.product.is_active || (!detail.product.images_customized && !dirty)" @click="save([], true)"><RotateCcw class="h-4 w-4" />{{ tr('restoreImages') }}</Button>
      </div>
    </div>
    <p v-if="error" role="alert" class="mt-3 text-sm text-destructive">{{ error }}</p>
    <p v-if="busy" role="status" class="mt-3 flex items-center gap-2 text-sm text-muted-foreground"><LoaderCircle class="h-4 w-4 animate-spin" />{{ tr('updatingImages') }}</p>
    <p v-else-if="success" role="status" class="mt-3 text-sm text-success">{{ success }}</p>
    <div class="mt-4 flex flex-wrap gap-4">
      <div v-for="(url, index) in images" :key="`${index}:${url}`" class="w-32">
        <div class="relative flex h-32 items-center justify-center overflow-hidden rounded-lg border bg-muted/30">
          <img :src="getImageUrl(url)" :alt="`${title} ${index + 1}`" class="h-full w-full object-contain" />
          <span v-if="index === 0" class="absolute left-1 top-1 rounded bg-background/90 px-2 py-1 text-xs">{{ tr('cover') }}</span>
        </div>
        <div class="mt-1 flex justify-between">
          <Button size="icon" variant="ghost" :aria-label="tr('moveLeft')" :title="tr('moveLeft')" :disabled="busy || !detail.product.is_active || index === 0" @click="move(index, -1)"><ArrowLeft class="h-4 w-4" /></Button>
          <Button size="icon" variant="ghost" class="text-destructive" :aria-label="tr('removeImage')" :title="tr('removeImage')" :disabled="busy || !detail.product.is_active" @click="remove(index)"><Trash2 class="h-4 w-4" /></Button>
          <Button size="icon" variant="ghost" :aria-label="tr('moveRight')" :title="tr('moveRight')" :disabled="busy || !detail.product.is_active || index === images.length - 1" @click="move(index, 1)"><ArrowRight class="h-4 w-4" /></Button>
        </div>
      </div>
      <div v-if="images.length === 0" class="w-full rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">{{ tr('noImages') }}</div>
    </div>
    <p class="mt-4 text-xs text-muted-foreground">{{ tr('imageHint') }}</p>
    <p class="mt-1 text-xs text-muted-foreground">{{ tr('imageAutoSaveHint') }}</p>
    <div v-if="dirty && !busy" class="mt-4 flex items-center justify-end gap-3">
      <span class="text-xs text-muted-foreground">{{ tr('unsavedImages') }}</span>
      <Button size="sm" :disabled="!detail.product.is_active" @click="save(images)">{{ tr('retrySaveImages') }}</Button>
    </div>
    <input ref="fileInput" type="file" multiple accept="image/jpeg,image/png,image/webp,image/gif" class="hidden" @change="upload" />
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, ArrowRight, ImagePlus, LoaderCircle, RotateCcw, Trash2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { resellerAPI } from '../../api/reseller'
import type { ResellerProductSettingDetailData } from '../../api/types'
import { getLocalizedText } from '../../utils/resellerSiteConfig'
import { getImageUrl } from '../../utils/image'
import { resellerImageSelectionError } from '../../utils/resellerProductSettings'

const props = defineProps<{ detail: ResellerProductSettingDetailData }>()
const emit = defineEmits<{ (event: 'saved', detail: ResellerProductSettingDetailData): void; (event: 'dirty', value: boolean): void }>()
const { t, locale } = useI18n()
const tr = (key: string) => t(`personalCenter.reseller.productSettings.${key}`)
const title = computed(() => getLocalizedText(props.detail.product.title, String(locale.value)))
const images = ref<string[]>([])
const original = ref<string[]>([])
const fileInput = ref<HTMLInputElement | null>(null)
const busy = ref(false)
const error = ref('')
const success = ref('')
const dirty = computed(() => JSON.stringify(images.value) !== JSON.stringify(original.value))
const inherited = computed(() => !props.detail.product.images_customized && !dirty.value)
watch(() => props.detail.product.images, (value) => {
  if (dirty.value) return
  images.value = [...(value || [])]; original.value = [...images.value]
}, { immediate: true })
watch(() => dirty.value || busy.value, (value) => emit('dirty', value))
const move = (index: number, delta: number) => {
  const other = index + delta
  if (other < 0 || other >= images.value.length || busy.value) return
  const next = [...images.value]
  ;[next[index], next[other]] = [next[other]!, next[index]!]
  void save(next)
}
const remove = (index: number) => {
  if (busy.value) return
  void save(images.value.filter((_, imageIndex) => imageIndex !== index))
}
const upload = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (!files.length || busy.value || !props.detail.product.is_active) return
  error.value = ''; success.value = ''
  // First customization replaces inherited artwork; failed first uploads leave it visible.
  const next = inherited.value ? [] : [...images.value]
  const selectionError = resellerImageSelectionError(files, next.length)
  if (selectionError) { error.value = tr(selectionError); return }
  busy.value = true
  try {
    for (const file of files) {
      const response = await resellerAPI.uploadImage(file)
      const url = response.data?.data?.url
      if (typeof url !== 'string' || !url) throw new Error(t('personalCenter.reseller.siteConfig.uploadFailed'))
      next.push(url)
      images.value = [...next]
    }
    await persist(next, false)
  } catch (err: any) { error.value = err?.message || tr('imagesFailed') }
  finally { busy.value = false }
}
const persist = async (next: string[], restore: boolean) => {
  const response = await resellerAPI.updateProductImages(props.detail.product.id, [...next])
  const detail = response.data?.data as ResellerProductSettingDetailData
  if (!detail?.product || !Array.isArray(detail.skus)) throw new Error(tr('imagesFailed'))
  images.value = [...(detail.product.images || [])]; original.value = [...images.value]
  emit('saved', detail)
  success.value = tr(restore || next.length === 0 ? 'imagesRestored' : 'imagesSaved')
}
const save = async (next: string[], restore = false) => {
  if (busy.value || !props.detail.product.is_active) return
  busy.value = true; error.value = ''; success.value = ''
  images.value = [...next]
  try {
    await persist(next, restore)
  } catch (err: any) { error.value = err?.message || tr('imagesFailed') }
  finally { busy.value = false }
}
</script>
