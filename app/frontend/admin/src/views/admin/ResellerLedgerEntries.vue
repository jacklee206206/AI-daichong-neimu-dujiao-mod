<!-- Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md. -->

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { adminAPI } from '@/api/admin'
import type { AdminResellerLedgerEntry } from '@/api/types'
import {
  RESELLER_LEDGER_STATUS_AVAILABLE,
  RESELLER_LEDGER_STATUS_CANCELED,
  RESELLER_LEDGER_STATUS_LOCKED,
  RESELLER_LEDGER_STATUS_PENDING_CONFIRM,
  RESELLER_LEDGER_STATUS_WITHDRAWN,
  RESELLER_LEDGER_TYPE_MANUAL_ADJUST,
  RESELLER_LEDGER_TYPE_ORDER_PROFIT,
  RESELLER_LEDGER_TYPE_REFUND_DEDUCT,
  RESELLER_LEDGER_TYPE_WITHDRAW_LOCK,
  RESELLER_LEDGER_TYPE_WITHDRAW_PAID,
} from '@/constants/reseller'
import IdCell from '@/components/IdCell.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import TableSkeleton from '@/components/TableSkeleton.vue'
import ListPagination from '@/components/ListPagination.vue'
import { useListRefresh, type ListFetchOptions } from '@/composables/useListRefresh'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { formatDate, toRFC3339 } from '@/utils/format'
import ComplianceGuardWrapper from '@/components/ComplianceGuardWrapper.vue'
import { adminUrl } from '@/utils/adminBase'
import { useAdminAuthStore } from '@/stores/auth'
import { notifyError, notifySuccess } from '@/utils/notify'
import { Dialog, DialogHeader, DialogScrollContent, DialogTitle } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { canReviewResellerProfit, resellerReviewStateKey } from '@/utils/resellerSettlement'

const { t } = useI18n()
const route = useRoute()
const auth = useAdminAuthStore()
const selectedEntry = ref<AdminResellerLedgerEntry | null>(null)
const confirmDialogOpen = ref(false)
const confirmationReason = ref('')
const confirming = ref(false)
const confirmPermission = computed(() => auth.hasPermission('POST:/admin/resellers/ledger-entries/:id/confirm'))
const canConfirm = (entry: AdminResellerLedgerEntry) => canReviewResellerProfit(entry, confirmPermission.value)
const reviewLabel = (entry: AdminResellerLedgerEntry) => t(`admin.resellerSettlement.states.${resellerReviewStateKey(entry.review_state)}`)
const openConfirmation = (entry: AdminResellerLedgerEntry) => {
  if (!canConfirm(entry)) return
  selectedEntry.value = entry
  confirmationReason.value = ''
  confirmDialogOpen.value = true
}
const submitConfirmation = async () => {
  const entry = selectedEntry.value
  if (!entry || !canConfirm(entry) || !confirmationReason.value.trim() || confirming.value) return
  confirming.value = true
  try {
    await adminAPI.confirmResellerLedgerEntry(entry.id, { reason: confirmationReason.value.trim() })
    confirmDialogOpen.value = false
    notifySuccess(t('admin.resellerSettlement.success'))
    await fetchRows(pagination.value.page, { preserveRows: true })
  } catch (error: any) {
    notifyError(error?.message || t('admin.resellerSettlement.failed'))
    await fetchRows(pagination.value.page, { preserveRows: true })
    const fresh = rows.value.find((row) => row.id === entry.id)
    if (fresh) selectedEntry.value = fresh
    else confirmDialogOpen.value = false
  } finally {
    confirming.value = false
  }
}
const loading = ref(true)
const { refreshing, refreshList } = useListRefresh()
const rows = ref<AdminResellerLedgerEntry[]>([])
const pagination = ref({
  page: 1,
  page_size: 20,
  total: 0,
  total_page: 1,
})

const filters = reactive({
  keyword: '',
  resellerId: '',
  userId: '',
  orderId: '',
  orderNo: '',
  type: '__all__',
  status: '__all__',
  createdFrom: '',
  createdTo: '',
})

const queryString = (value: unknown) => (Array.isArray(value) ? value[0] : value)

const initFiltersFromQuery = () => {
  const resellerId = String(queryString(route.query.reseller_id) || '').trim()
  if (resellerId) filters.resellerId = resellerId
}

const normalizeFilterValue = (value: string) => (value === '__all__' ? '' : value)
const pageSizeOptions = [10, 20, 50, 100]
const userDetailLink = (userId: number) => adminUrl(`/users/${userId}`)
const orderLink = (orderNo: string) => adminUrl(`/orders?order_no=${encodeURIComponent(orderNo)}`)

const fetchRows = async (page = 1, options: ListFetchOptions = {}) => {
  if (!options.preserveRows) loading.value = true
  try {
    const response = await adminAPI.getResellerLedgerEntries({
      page,
      page_size: pagination.value.page_size,
      keyword: filters.keyword || undefined,
      reseller_id: filters.resellerId || undefined,
      user_id: filters.userId || undefined,
      order_id: filters.orderId || undefined,
      order_no: filters.orderNo || undefined,
      type: normalizeFilterValue(filters.type) || undefined,
      status: normalizeFilterValue(filters.status) || undefined,
      created_from: toRFC3339(filters.createdFrom),
      created_to: toRFC3339(filters.createdTo),
    })
    rows.value = response.data.data || []
    pagination.value = response.data.pagination || pagination.value
  } catch {
    if (!options.preserveRows) rows.value = []
  } finally {
    if (!options.preserveRows) loading.value = false
  }
}

const handleSearch = () => {
  fetchRows(1, { preserveRows: true })
}
const debouncedSearch = useDebounceFn(handleSearch, 300)

const refreshCurrentPage = () => {
  refreshList(() => fetchRows(pagination.value.page, { preserveRows: true }))
}

const changePage = (page: number) => {
  if (page < 1 || page > pagination.value.total_page) return
  fetchRows(page)
}

const changePageSize = (size: number) => {
  if (size === pagination.value.page_size) return
  pagination.value.page_size = size
  fetchRows(1)
}

const resolveResellerUserID = (item: AdminResellerLedgerEntry) => Number(item?.profile?.user_id || item?.profile?.user?.id || 0)

const ledgerTypeLabel = (type?: string) => {
  if (type === RESELLER_LEDGER_TYPE_ORDER_PROFIT) return t('admin.resellerLedgerEntries.types.order_profit')
  if (type === RESELLER_LEDGER_TYPE_REFUND_DEDUCT) return t('admin.resellerLedgerEntries.types.refund_deduct')
  if (type === RESELLER_LEDGER_TYPE_MANUAL_ADJUST) return t('admin.resellerLedgerEntries.types.manual_adjust')
  if (type === RESELLER_LEDGER_TYPE_WITHDRAW_LOCK) return t('admin.resellerLedgerEntries.types.withdraw_lock')
  if (type === RESELLER_LEDGER_TYPE_WITHDRAW_PAID) return t('admin.resellerLedgerEntries.types.withdraw_paid')
  return type || '-'
}

const statusLabel = (status?: string) => {
  if (status === RESELLER_LEDGER_STATUS_PENDING_CONFIRM) return t('admin.resellerLedgerEntries.status.pending_confirm')
  if (status === RESELLER_LEDGER_STATUS_AVAILABLE) return t('admin.resellerLedgerEntries.status.available')
  if (status === RESELLER_LEDGER_STATUS_LOCKED) return t('admin.resellerLedgerEntries.status.locked')
  if (status === RESELLER_LEDGER_STATUS_WITHDRAWN) return t('admin.resellerLedgerEntries.status.withdrawn')
  if (status === RESELLER_LEDGER_STATUS_CANCELED) return t('admin.resellerLedgerEntries.status.canceled')
  return status || '-'
}

const statusClass = (status?: string) => {
  if (status === RESELLER_LEDGER_STATUS_PENDING_CONFIRM) return 'border-amber-200 bg-amber-50 text-amber-700'
  if (status === RESELLER_LEDGER_STATUS_AVAILABLE) return 'border-emerald-200 bg-emerald-50 text-emerald-700'
  if (status === RESELLER_LEDGER_STATUS_LOCKED) return 'border-sky-200 bg-sky-50 text-sky-700'
  if (status === RESELLER_LEDGER_STATUS_WITHDRAWN) return 'border-zinc-200 bg-zinc-50 text-zinc-700'
  if (status === RESELLER_LEDGER_STATUS_CANCELED) return 'border-border bg-muted/30 text-muted-foreground'
  return 'border-border bg-muted/30 text-muted-foreground'
}

onMounted(() => {
  initFiltersFromQuery()
  fetchRows()
})
</script>

<template>
  <ComplianceGuardWrapper>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div><h1 class="text-2xl font-semibold">{{ t('admin.resellerLedgerEntries.title') }}</h1><p class="mt-2 text-sm text-muted-foreground">{{ t('admin.resellerSettlement.hint') }}</p></div>
      </div>

      <div class="rounded-xl border border-border bg-card p-4 shadow-sm">
        <div class="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
          <div class="w-full md:w-56">
            <Input v-model="filters.keyword" :placeholder="t('admin.resellerLedgerEntries.filters.keyword')" @update:modelValue="debouncedSearch" />
          </div>
          <div class="w-full md:w-36">
            <Input v-model="filters.resellerId" :placeholder="t('admin.resellerLedgerEntries.filters.resellerId')" @update:modelValue="debouncedSearch" />
          </div>
          <div class="w-full md:w-32">
            <Input v-model="filters.userId" :placeholder="t('admin.resellerLedgerEntries.filters.userId')" @update:modelValue="debouncedSearch" />
          </div>
          <div class="w-full md:w-36">
            <Input v-model="filters.orderId" :placeholder="t('admin.resellerLedgerEntries.filters.orderId')" @update:modelValue="debouncedSearch" />
          </div>
          <div class="w-full md:w-52">
            <Input v-model="filters.orderNo" :placeholder="t('admin.resellerLedgerEntries.filters.orderNo')" @update:modelValue="debouncedSearch" />
          </div>
          <div class="w-full md:w-44">
            <Select v-model="filters.type" @update:modelValue="handleSearch">
              <SelectTrigger class="h-9 w-full">
                <SelectValue :placeholder="t('admin.resellerLedgerEntries.filters.typeAll')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">{{ t('admin.resellerLedgerEntries.filters.typeAll') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_TYPE_ORDER_PROFIT">{{ t('admin.resellerLedgerEntries.types.order_profit') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_TYPE_REFUND_DEDUCT">{{ t('admin.resellerLedgerEntries.types.refund_deduct') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_TYPE_MANUAL_ADJUST">{{ t('admin.resellerLedgerEntries.types.manual_adjust') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_TYPE_WITHDRAW_LOCK">{{ t('admin.resellerLedgerEntries.types.withdraw_lock') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_TYPE_WITHDRAW_PAID">{{ t('admin.resellerLedgerEntries.types.withdraw_paid') }}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="w-full md:w-44">
            <Select v-model="filters.status" @update:modelValue="handleSearch">
              <SelectTrigger class="h-9 w-full">
                <SelectValue :placeholder="t('admin.resellerLedgerEntries.filters.statusAll')" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">{{ t('admin.resellerLedgerEntries.filters.statusAll') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_STATUS_PENDING_CONFIRM">{{ t('admin.resellerLedgerEntries.status.pending_confirm') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_STATUS_AVAILABLE">{{ t('admin.resellerLedgerEntries.status.available') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_STATUS_LOCKED">{{ t('admin.resellerLedgerEntries.status.locked') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_STATUS_WITHDRAWN">{{ t('admin.resellerLedgerEntries.status.withdrawn') }}</SelectItem>
                <SelectItem :value="RESELLER_LEDGER_STATUS_CANCELED">{{ t('admin.resellerLedgerEntries.status.canceled') }}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div class="flex w-full flex-col gap-2 md:w-auto md:flex-row md:flex-wrap md:items-center">
            <span class="text-xs text-muted-foreground whitespace-nowrap">{{ t('admin.resellerLedgerEntries.filters.createdRange') }}</span>
            <Input v-model="filters.createdFrom" type="datetime-local" class="h-9 w-full md:w-auto" @update:modelValue="handleSearch" />
            <span class="hidden text-muted-foreground md:inline">-</span>
            <Input v-model="filters.createdTo" type="datetime-local" class="h-9 w-full md:w-auto" @update:modelValue="handleSearch" />
          </div>
          <div class="hidden flex-1 sm:block"></div>
          <Button size="sm" variant="outline" class="w-full sm:w-auto" :disabled="refreshing" @click="refreshCurrentPage">{{ t('admin.common.refresh') }}</Button>
        </div>
      </div>

      <div class="rounded-xl border border-border bg-card overflow-x-auto">
        <Table class="min-w-[1450px]">
          <TableHeader class="border-b border-border bg-muted/40 text-xs uppercase text-muted-foreground">
            <TableRow>
              <TableHead class="px-6 py-3">{{ t('admin.resellerLedgerEntries.table.id') }}</TableHead>
              <TableHead class="min-w-[180px] px-6 py-3">{{ t('admin.resellerLedgerEntries.table.reseller') }}</TableHead>
              <TableHead class="min-w-[160px] px-6 py-3">{{ t('admin.resellerLedgerEntries.table.orderNo') }}</TableHead>
              <TableHead class="min-w-[110px] px-6 py-3">{{ t('admin.resellerLedgerEntries.table.type') }}</TableHead>
              <TableHead class="px-6 py-3">{{ t('admin.resellerLedgerEntries.table.amount') }}</TableHead>
              <TableHead class="px-6 py-3">{{ t('admin.resellerLedgerEntries.table.currency') }}</TableHead>
              <TableHead class="min-w-[110px] px-6 py-3">{{ t('admin.resellerLedgerEntries.table.status') }}</TableHead>
              <TableHead class="min-w-[170px] px-6 py-3">{{ t('admin.resellerSettlement.deliveryAt') }}</TableHead>
              <TableHead class="min-w-[170px] px-6 py-3">{{ t('admin.resellerSettlement.reviewAt') }}</TableHead>
              <TableHead class="min-w-[190px] px-6 py-3">{{ t('admin.resellerSettlement.review') }}</TableHead>
              <TableHead class="min-w-[100px] px-6 py-3">{{ t('admin.resellerLedgerEntries.table.withdrawRequest') }}</TableHead>
              <TableHead class="min-w-[140px] px-6 py-3">{{ t('admin.resellerLedgerEntries.table.createdAt') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody class="divide-y divide-border">
            <TableRow v-if="loading">
              <TableCell :colspan="12" class="p-0">
                <TableSkeleton :columns="12" :rows="5" />
              </TableCell>
            </TableRow>
            <TableRow v-else-if="rows.length === 0">
              <TableCell colspan="12" class="px-6 py-8 text-center text-muted-foreground">{{ t('admin.resellerLedgerEntries.empty') }}</TableCell>
            </TableRow>
            <TableRow v-for="item in rows" :key="item.id" class="hover:bg-muted/30">
              <TableCell class="px-6 py-4">
                <IdCell :value="item.id" />
              </TableCell>
              <TableCell class="min-w-[180px] px-6 py-4 text-xs text-muted-foreground">
                <div class="text-foreground break-words">{{ item?.profile?.user?.display_name || '-' }}</div>
                <div v-if="item?.profile?.user?.email" class="mt-0.5 break-all">{{ item.profile.user.email }}</div>
                <div class="mt-0.5 break-all">
                  <a
                    v-if="resolveResellerUserID(item) > 0"
                    :href="userDetailLink(resolveResellerUserID(item))"
                    target="_blank"
                    rel="noopener"
                    class="font-mono text-primary underline-offset-4 hover:underline"
                  >
                    #{{ resolveResellerUserID(item) }}
                  </a>
                  <span v-else class="font-mono text-foreground">-</span>
                  <span class="ml-1 font-mono text-muted-foreground">/ R#{{ item.reseller_id }}</span>
                </div>
              </TableCell>
              <TableCell class="min-w-[160px] px-6 py-4 font-mono text-xs text-foreground break-all">
                <a v-if="item?.order?.order_no" :href="orderLink(item.order.order_no)" target="_blank" rel="noopener" class="text-primary underline-offset-4 hover:underline">
                  {{ item.order.order_no }}
                </a>
                <span v-else>-</span>
              </TableCell>
              <TableCell class="min-w-[110px] px-6 py-4 text-xs text-foreground">{{ ledgerTypeLabel(item.type) }}</TableCell>
              <TableCell class="px-6 py-4 font-mono text-xs text-foreground">{{ item.amount || '0.00' }}</TableCell>
              <TableCell class="px-6 py-4 font-mono text-xs text-muted-foreground">{{ item.currency || '-' }}</TableCell>
              <TableCell class="min-w-[110px] px-6 py-4 text-xs">
                <span class="inline-flex rounded-full border px-2.5 py-1 text-xs" :class="statusClass(item.status)">
                  {{ statusLabel(item.status) }}
                </span>
              </TableCell>
              <TableCell class="px-6 py-4 text-xs text-muted-foreground">{{ formatDate(item.delivery_completed_at) || '-' }}</TableCell>
              <TableCell class="px-6 py-4 text-xs text-muted-foreground">{{ formatDate(item.available_at) || '-' }}</TableCell>
              <TableCell class="px-6 py-4 text-xs">
                <div>{{ reviewLabel(item) }}</div>
                <Button v-if="item.type === 'order_profit' && item.status === 'pending_confirm' && item.confirmation_mode === 'manual' && item.can_confirm && confirmPermission" size="sm" class="mt-2" :disabled="!canConfirm(item) || confirming" @click="openConfirmation(item)">{{ t('admin.resellerSettlement.confirm') }}</Button>
                <div v-if="item.confirmed_at" class="mt-1 text-muted-foreground">{{ formatDate(item.confirmed_at) }} · {{ item.confirmed_by ? `#${item.confirmed_by}` : t('admin.resellerSettlement.automatic') }}</div>
                <div v-if="item.confirmation_reason" class="mt-1 max-w-64 whitespace-pre-wrap break-words text-muted-foreground">{{ item.confirmation_reason }}</div>
              </TableCell>
              <TableCell class="min-w-[100px] px-6 py-4 font-mono text-xs text-foreground">
                <span v-if="item.withdraw_request_id">#{{ item.withdraw_request_id }}</span>
                <span v-else>-</span>
              </TableCell>
              <TableCell class="min-w-[140px] px-6 py-4 text-xs text-muted-foreground">{{ formatDate(item.created_at) }}</TableCell>
            </TableRow>
          </TableBody>
        </Table>

        <ListPagination
          :page="pagination.page"
          :total-page="pagination.total_page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          :page-size-options="pageSizeOptions"
          @change-page="changePage"
          @change-page-size="changePageSize"
        />
      </div>
      <Dialog v-model:open="confirmDialogOpen">
        <DialogScrollContent class="sm:max-w-lg">
          <DialogHeader><DialogTitle>{{ t('admin.resellerSettlement.confirm') }}</DialogTitle></DialogHeader>
          <template v-if="selectedEntry">
            <p class="text-sm text-muted-foreground">{{ t('admin.resellerSettlement.confirmHint') }}</p>
            <div class="rounded-lg border bg-muted/30 p-4 text-sm">
              <p class="font-mono">{{ selectedEntry.order?.order_no || selectedEntry.order_id }} · R#{{ selectedEntry.reseller_id }}</p>
              <p class="mt-2 font-mono font-semibold">{{ selectedEntry.amount }} {{ selectedEntry.currency }}</p>
              <p class="mt-2 text-muted-foreground">{{ t('admin.resellerSettlement.deliveryAt') }}: {{ formatDate(selectedEntry.delivery_completed_at) }}</p>
              <p class="mt-1 text-muted-foreground">{{ t('admin.resellerSettlement.reviewAt') }}: {{ formatDate(selectedEntry.available_at) }}</p>
            </div>
            <label for="reseller-profit-reason" class="text-sm font-medium">{{ t('admin.resellerSettlement.reason') }}</label>
            <Textarea id="reseller-profit-reason" v-model="confirmationReason" maxlength="1000" :disabled="confirming" :placeholder="t('admin.resellerSettlement.reasonPlaceholder')" />
            <div class="flex justify-end gap-2">
              <Button variant="outline" :disabled="confirming" @click="confirmDialogOpen = false">{{ t('admin.common.cancel') }}</Button>
              <Button :disabled="confirming || !canConfirm(selectedEntry) || !confirmationReason.trim()" @click="submitConfirmation">{{ t(confirming ? 'admin.resellerSettlement.confirming' : 'admin.resellerSettlement.confirm') }}</Button>
            </div>
          </template>
        </DialogScrollContent>
      </Dialog>
    </div>
  </ComplianceGuardWrapper>
</template>
