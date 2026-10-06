<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <div v-if="items.length" class="space-y-2">
    <button v-for="item in items" :key="item.key" type="button" class="flex w-full items-center gap-2 rounded-lg border bg-secondary p-3 text-left text-sm text-muted-foreground hover:text-foreground" @click="copy(item.value)">
      <Copy class="h-4 w-4 shrink-0" /><span class="min-w-0 break-all">{{ item.label }}：{{ item.value }}</span>
    </button>
    <p v-if="message" role="status" class="text-xs text-muted-foreground">{{ message }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy } from 'lucide-vue-next'
const props = defineProps<{ contact?: { wechat?: string; qq?: string; email?: string } | null }>()
const { t } = useI18n()
const message = ref('')
const items = computed(() => (['wechat','qq','email'] as const).map((key) => ({key, label:t(`personalCenter.reseller.siteConfig.fields.${key}`),value:String(props.contact?.[key] || '')})).filter((item) => item.value))
const copy = async (value: string) => {
  try { await navigator.clipboard.writeText(value); message.value=t('resellerConsole.common.copied') }
  catch { message.value=value }
}
</script>
