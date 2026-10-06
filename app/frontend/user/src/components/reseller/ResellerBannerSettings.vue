<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <section class="space-y-5 rounded-2xl border bg-card p-4 sm:p-5">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="flex items-start gap-3"><span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary"><PanelsTopLeft class="h-5 w-5" /></span><div><h3 class="text-base font-bold">{{ tr('title') }}</h3><p class="mt-1 text-sm text-muted-foreground">{{ tr('description') }}</p></div></div>
      <Button type="button" variant="outline" size="sm" @click="update({ mode: 'inherit' })"><RotateCcw class="h-4 w-4" />{{ tr('restore') }}</Button>
    </div>
    <div class="grid gap-3 md:grid-cols-3" role="radiogroup" :aria-label="tr('title')">
      <button v-for="mode in modes" :key="mode" type="button" role="radio" :aria-checked="value.mode === mode" class="rounded-xl border p-4 text-left" :class="value.mode === mode ? 'border-primary bg-primary/5 ring-1 ring-primary' : 'bg-muted/20'" @click="update({ mode })">
        <span class="flex items-center justify-between gap-2 font-semibold">{{ tr(mode) }}<span class="h-2.5 w-2.5 shrink-0 rounded-full" :class="value.mode === mode ? 'bg-primary' : 'bg-muted-foreground/30'"></span></span><span class="mt-2 block text-xs text-muted-foreground">{{ tr(`${mode}Hint`) }}</span>
      </button>
    </div>
    <template v-if="value.mode === 'custom'">
      <div class="grid gap-5 md:grid-cols-2">
        <ResellerImageField :model-value="value.desktop_image" wide :label="tr('desktop')" :hint="tr('desktopHint')" @update:model-value="update({ desktop_image: $event })" />
        <ResellerImageField :model-value="value.mobile_image" wide :label="tr('mobile')" :hint="tr('mobileHint')" @update:model-value="update({ mobile_image: $event })" />
      </div>
      <div class="rounded-xl border bg-muted/20 p-4">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3"><h4 class="font-medium">{{ tr('copy') }}</h4><ResellerLocaleTabs v-model="locale" /></div>
        <div class="grid gap-4 md:grid-cols-2">
          <label class="text-sm"><span class="mb-2 block text-muted-foreground">{{ tr('heading') }}</span><Input :model-value="value.title[locale]" maxlength="120" @update:model-value="update({ title: { ...value.title, [locale]: String($event) } })" /></label>
          <label class="text-sm"><span class="mb-2 block text-muted-foreground">{{ tr('subtitle') }}</span><Input :model-value="value.subtitle[locale]" maxlength="240" @update:model-value="update({ subtitle: { ...value.subtitle, [locale]: String($event) } })" /></label>
        </div>
      </div>
      <label class="block text-sm"><span class="mb-2 block text-muted-foreground">{{ tr('link') }}</span><Input :model-value="value.link_url" type="text" placeholder="https://…" @update:model-value="update({ link_url: String($event).trim() })" /><span class="mt-1 block text-xs text-muted-foreground">{{ tr('linkHint') }}</span></label>
      <p v-if="value.desktop_image" class="text-sm text-muted-foreground">{{ tr('preview') }}</p>
      <div v-if="value.desktop_image" class="overflow-hidden rounded-xl border">
        <div class="relative flex aspect-[16/5] min-h-40 items-center overflow-hidden bg-muted p-6 sm:p-8">
          <img :src="getImageUrl(value.desktop_image)" alt="" class="absolute inset-0 h-full w-full object-cover" />
          <span class="absolute inset-0 bg-black/35"></span>
          <div class="relative max-w-full text-white"><h4 class="break-words text-2xl font-bold sm:text-4xl">{{ value.title[locale] }}</h4><p class="mt-3 break-words text-sm sm:text-lg">{{ value.subtitle[locale] }}</p></div>
        </div>
      </div>
    </template>
    <p v-else class="rounded-xl border border-dashed p-5 text-sm text-muted-foreground">{{ tr(value.mode === 'hidden' ? 'hiddenPreview' : 'inheritPreview') }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { PanelsTopLeft, RotateCcw } from 'lucide-vue-next'
import ResellerImageField from './ResellerImageField.vue'
import ResellerLocaleTabs from './ResellerLocaleTabs.vue'
import type { ResellerBannerConfig } from '../../api/types'
import { normalizeResellerBanner, type ResellerLocale } from '../../utils/resellerSiteConfig'
import { getImageUrl } from '../../utils/image'
const props = defineProps<{ modelValue?: ResellerBannerConfig }>()
const emit = defineEmits<{ (event: 'update:modelValue', value: ResellerBannerConfig): void }>()
const { t } = useI18n()
const tr = (key: string) => t(`resellerConsole.banner.${key}`)
const modes = ['inherit', 'custom', 'hidden'] as const
const locale = ref<ResellerLocale>('zh-CN')
const value = computed(() => normalizeResellerBanner(props.modelValue))
const update = (patch: Partial<ResellerBannerConfig>) => emit('update:modelValue', { ...value.value, ...patch })
</script>
