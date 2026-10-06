<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<template>
  <div v-if="estimate">
  <section class="mt-4 rounded-xl border border-primary/30 bg-primary/5 p-4 text-sm" aria-live="polite" data-testid="gateway-fee-disclosure">
    <h3 class="font-semibold text-foreground">{{ t('payment.gatewayFeeTitle') }}</h3>
    <dl class="mt-3 space-y-2">
      <div class="flex justify-between gap-4"><dt>{{ t('payment.goodsAmountLabel') }}</dt><dd>{{ money(estimate.orderCents) }}</dd></div>
      <div v-if="estimate.walletCents > 0" class="flex justify-between gap-4"><dt>{{ t('payment.walletDeductLabel') }}</dt><dd>−{{ money(estimate.walletCents) }}</dd></div>
      <div v-if="estimate.shopFeeCents > 0" class="flex justify-between gap-4"><dt>{{ t('payment.shopFeeLabel') }}</dt><dd>{{ money(estimate.shopFeeCents) }}</dd></div>
      <div class="flex justify-between gap-4"><dt>{{ t('payment.gatewayFeeLabel') }} <span class="text-muted-foreground">({{ rateLabel }})</span></dt><dd>{{ money(estimate.gatewayFeeCents) }}</dd></div>
      <div class="flex justify-between gap-4 border-t pt-2 font-semibold text-foreground"><dt>{{ t('payment.expectedOnlinePayableLabel') }}</dt><dd>{{ money(estimate.onlinePayableCents) }}</dd></div>
      <div v-if="estimate.walletCents > 0" class="flex justify-between gap-4"><dt>{{ t('payment.expectedTotalCostLabel') }}</dt><dd>{{ money(estimate.totalCents) }}</dd></div>
    </dl>

  </section>
  <CryptoTransferNotice v-if="channel?.provider_type === 'dujiaopay'" class="mt-4" :fallback-token-id="channel.crypto_token_id" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import CryptoTransferNotice from './CryptoTransferNotice.vue'
import { useI18n } from 'vue-i18n'
import { estimateGatewayCustomerFee } from '../../utils/gatewayCustomerFee'
import { amountToCents, basisPointsToPercent, centsToAmount, rateToBasisPoints } from '../../utils/money'

const props = defineProps<{
  channel?: any
  orderAmount?: string
  walletAmount?: string
  currency?: string
  formatMoney: (amount?: string, currency?: string) => string
}>()
const { t } = useI18n()
const estimate = computed(() => estimateGatewayCustomerFee(props.channel, props.orderAmount, props.walletAmount))
const money = (cents: number) => props.formatMoney(centsToAmount(cents), props.currency)
const rateLabel = computed(() => {
  const rate = rateToBasisPoints(props.channel?.gateway_customer_fee_rate) || 0
  const fixed = amountToCents(props.channel?.gateway_customer_fixed_fee) || 0
  return [rate > 0 ? `${basisPointsToPercent(rate)}%` : '', fixed > 0 ? money(fixed) : ''].filter(Boolean).join(' + ') || '0.00%'
})
</script>
