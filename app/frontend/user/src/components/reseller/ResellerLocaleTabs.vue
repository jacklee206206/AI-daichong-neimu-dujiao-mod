<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <div class="inline-flex rounded-lg border border-border bg-muted/40 p-0.5">
    <button
      v-for="loc in locales"
      :key="loc"
      type="button"
      class="rounded-md px-3 py-1 text-xs font-semibold transition-colors"
      :class="
        loc === modelValue
          ? 'bg-background text-foreground shadow-sm'
          : 'text-muted-foreground hover:text-foreground'
      "
      @click="emit('update:modelValue', loc)"
    >
      {{ labels?.[loc] ?? defaultLabels[loc] }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { resellerLocales, type ResellerLocale } from '../../utils/resellerSiteConfig'

defineProps<{ modelValue: ResellerLocale; labels?: Record<string, string> }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: ResellerLocale): void }>()

const locales = resellerLocales
const defaultLabels: Record<ResellerLocale, string> = { 'zh-CN': '简体', 'zh-TW': '繁體', 'en-US': 'EN' }
</script>
