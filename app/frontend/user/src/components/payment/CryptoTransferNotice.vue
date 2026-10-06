<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <section :class="isBscUsdt ? 'text-sm' : 'rounded-xl border border-amber-300/70 bg-amber-50/80 p-4 text-sm text-amber-950 dark:border-amber-700 dark:bg-amber-950/25 dark:text-amber-100'" data-testid="crypto-transfer-notice">
    <template v-if="isBscUsdt">
      <div v-if="details.exactAmount" class="mb-3">
        <p class="text-xs font-semibold text-muted-foreground">{{ t('payment.cryptoTransfer.receivable') }}</p>
        <p class="mt-1 break-all text-3xl font-bold tabular-nums text-emerald-600 dark:text-emerald-400">{{ details.exactAmount }} USDT</p>
      </div>
      <p class="inline-flex rounded-full border border-blue-200 bg-blue-50 px-3 py-1 text-xs font-semibold text-blue-700 dark:border-blue-800 dark:bg-blue-950/40 dark:text-blue-300">{{ t('payment.cryptoTransfer.bsc.network') }}</p>
      <p v-if="details.exactAmount" class="mt-2 rounded-full border border-blue-200 bg-blue-50 px-3 py-1 text-xs font-semibold text-blue-700 dark:border-blue-800 dark:bg-blue-950/40 dark:text-blue-300">{{ t('payment.cryptoTransfer.bsc.exactMatch') }}</p>
      <div class="mt-3 rounded-xl border border-amber-300/70 bg-amber-50/80 p-4 leading-relaxed text-amber-950 dark:border-amber-700 dark:bg-amber-950/25 dark:text-amber-100">
        <h3 class="font-bold">{{ t('payment.cryptoTransfer.amountTitle') }}</h3>
        <i18n-t v-if="details.exactAmount" keypath="payment.cryptoTransfer.bsc.amountExact" tag="p" class="mt-2">
          <template #amount><strong>{{ details.exactAmount }} USDT</strong></template>
        </i18n-t>
        <p v-else class="mt-2">{{ t('payment.cryptoTransfer.bsc.amountPending') }}</p>
        <p class="mt-6"><strong>{{ t('payment.cryptoTransfer.bsc.methodTitle') }}</strong>{{ t('payment.cryptoTransfer.bsc.method') }}</p>
      </div>
    </template>
    <template v-else>
      <h3 class="font-bold">{{ t('payment.cryptoTransfer.title', { token: tokenLabel }) }}</h3>
      <div v-if="details.exactAmount" class="mt-3 rounded-lg border border-emerald-200 bg-white p-3 dark:border-emerald-800 dark:bg-background">
        <p class="text-xs text-muted-foreground">{{ t('payment.cryptoTransfer.receivable') }}</p>
        <p class="mt-1 break-all text-2xl font-bold tabular-nums text-emerald-600 dark:text-emerald-400">{{ details.exactAmount }} {{ details.tokenLabel }}</p>
      </div>
      <p v-if="details.networkLabel" class="mt-3 inline-flex rounded-full border border-blue-200 bg-blue-50 px-3 py-1 text-xs font-semibold text-blue-700 dark:border-blue-800 dark:bg-blue-950/40 dark:text-blue-300">{{ details.networkLabel }}</p>
      <ol class="mt-3 space-y-4 leading-relaxed">
        <li>
          <h4 class="font-semibold">1. {{ t('payment.cryptoTransfer.amountTitle') }}</h4>
          <p class="mt-1">{{ details.exactAmount ? t('payment.cryptoTransfer.amountExact', { amount: details.exactAmount, token: details.tokenLabel }) : t('payment.cryptoTransfer.amountPending', { token: tokenLabel }) }}</p>
          <p class="mt-1">{{ t('payment.cryptoTransfer.exchangeFee') }}</p>
          <p v-if="details.isBsc" class="mt-1">{{ t('payment.cryptoTransfer.bscGas', { token: tokenLabel }) }}</p>
        </li>
        <li>
          <h4 class="font-semibold">2. {{ t('payment.cryptoTransfer.networkTitle') }}</h4>
          <p class="mt-1">{{ details.networkLabel ? t('payment.cryptoTransfer.networkExact', { network: details.networkLabel }) : t('payment.cryptoTransfer.networkPending') }}</p>
        </li>
        <li>
          <h4 class="font-semibold">3. {{ t('payment.cryptoTransfer.methodTitle') }}</h4>
          <p class="mt-1">{{ t('payment.cryptoTransfer.method') }}</p>
          <p class="mt-1">{{ t('payment.cryptoTransfer.txid') }}</p>
        </li>
      </ol>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { resolveCryptoTransferDetails } from '../../utils/cryptoTransferNotice'
const props = defineProps<{ chain?: string; tokenId?: string; chainAmount?: string; fallbackTokenId?: string }>()
const { t } = useI18n()
const details = computed(() => {
  // An actual payment response always takes priority over the channel's configured default.
  const hasPaymentIdentity = Boolean(props.chain?.trim() || props.tokenId?.trim())
  return resolveCryptoTransferDetails({
    chain: props.chain,
    tokenId: hasPaymentIdentity ? props.tokenId : props.fallbackTokenId,
    chainAmount: props.chainAmount,
  })
})
const isBscUsdt = computed(() => details.value.isBsc && details.value.tokenLabel === 'USDT')
const tokenLabel = computed(() => details.value.tokenLabel || t('payment.cryptoTransfer.tokenFallback'))
</script>
