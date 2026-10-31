<template>
  <section class="card p-4 sm:p-6" data-testid="cash-applications">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t(admin ? 'affiliate.cash.adminTitle' : 'affiliate.cash.history') }}</h3>
      <div class="flex items-center gap-2">
        <select v-model="status" class="input w-auto" :aria-label="t('affiliate.cash.filter')" @change="page = 1; load()">
          <option value="">{{ t('affiliate.cash.all') }}</option>
          <option v-for="value in statuses" :key="value" :value="value">{{ t(`affiliate.cash.status.${value}`) }}</option>
        </select>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="load">{{ t('common.refresh') }}</button>
      </div>
    </div>
    <p v-if="error" role="alert" class="mb-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    <p v-if="loading && !items.length" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
    <p v-else-if="!items.length" class="py-8 text-center text-sm text-gray-500">{{ t('affiliate.cash.empty') }}</p>
    <div v-else class="overflow-x-auto">
      <table class="w-full min-w-[620px] text-left text-sm">
        <thead><tr class="border-b border-gray-200 text-gray-500 dark:border-dark-700">
          <th class="px-3 py-3">{{ t('affiliate.cash.application') }}</th>
          <th v-if="admin" class="px-3 py-3">{{ t('affiliate.cash.applicant') }}</th>
          <th class="px-3 py-3">{{ t('affiliate.cash.amount') }}</th>
          <th class="px-3 py-3">{{ t('affiliate.cash.state') }}</th>
          <th class="px-3 py-3">{{ t('affiliate.cash.createdAt') }}</th>
          <th class="px-3 py-3"><span class="sr-only">{{ t('common.actions') }}</span></th>
        </tr></thead>
        <tbody><tr v-for="item in items" :key="item.id" class="border-b border-gray-100 last:border-0 dark:border-dark-800">
          <td class="px-3 py-3 font-mono">#{{ item.id }}</td>
          <td v-if="admin" class="px-3 py-3">{{ item.username || `#${item.user_id}` }}</td>
          <td class="whitespace-nowrap px-3 py-3 font-medium">{{ formatCashAmount(item.amount) }}</td>
          <td class="px-3 py-3"><span class="rounded-full px-2.5 py-1 text-xs" :class="statusClass(item.status)">{{ t(`affiliate.cash.status.${item.status}`) }}</span></td>
          <td class="whitespace-nowrap px-3 py-3 text-gray-500">{{ formatDateTime(item.created_at) }}</td>
          <td class="px-3 py-3"><button type="button" class="btn btn-secondary btn-sm whitespace-nowrap" @click="open(item.id)">{{ t(admin ? 'affiliate.cash.viewReply' : 'common.view') }}</button></td>
        </tr></tbody>
      </table>
    </div>
    <Pagination v-if="total > 10" :page="page" :page-size="10" :total="total" @update:page="page = $event; load()" />

    <BaseDialog :show="showDetail" :title="t('affiliate.cash.detailTitle')" width="wide" :show-close-button="!sending" :close-on-escape="!sending" @close="close">
      <p v-if="detailLoading" class="py-8 text-center">{{ t('common.loading') }}</p>
      <div v-else-if="detail" class="space-y-5">
        <div class="grid gap-5 sm:grid-cols-[1fr_200px]">
          <dl class="grid content-start gap-4 sm:grid-cols-2">
            <div><dt class="text-xs text-gray-500">{{ t('affiliate.cash.application') }}</dt><dd class="mt-1 font-mono">#{{ detail.id }}</dd></div>
            <div><dt class="text-xs text-gray-500">{{ t('affiliate.cash.amount') }}</dt><dd class="mt-1 font-semibold">{{ formatCashAmount(detail.amount) }}</dd></div>
            <div class="sm:col-span-2"><dt class="text-xs text-gray-500">{{ t('affiliate.cash.email') }}</dt><dd class="mt-1 break-all">{{ detail.email }}</dd></div>
            <div><dt class="text-xs text-gray-500">{{ t('affiliate.cash.state') }}</dt><dd class="mt-1">{{ t(`affiliate.cash.status.${detail.status}`) }}</dd></div>
            <div><dt class="text-xs text-gray-500">{{ t('affiliate.cash.createdAt') }}</dt><dd class="mt-1 text-sm">{{ formatDateTime(detail.created_at) }}</dd></div>
            <div class="sm:col-span-2"><dt class="text-xs text-gray-500">{{ t('affiliate.cash.payoutAfter') }}</dt><dd class="mt-1 text-sm">{{ formatDateTime(detail.payout_not_before) }}</dd></div>
          </dl>
          <figure class="space-y-2">
            <img v-if="qrURL" :src="qrURL" :alt="t('affiliate.cash.paymentQR')" class="mx-auto max-h-64 w-full rounded-xl bg-white object-contain p-2" />
            <figcaption class="text-center text-xs text-gray-500">{{ t('affiliate.cash.paymentQR') }}</figcaption>
            <button v-if="qrError" type="button" class="btn btn-secondary btn-sm w-full" @click="loadQR(detail.id)">{{ t('affiliate.cash.retryQR') }}</button>
          </figure>
        </div>
        <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
          <h4 class="font-medium">{{ t('affiliate.cash.replies') }}</h4>
          <p v-if="!detail.replies?.length" class="text-sm text-gray-500">{{ t('affiliate.cash.noReplies') }}</p>
          <article v-for="reply in detail.replies" :key="reply.id" class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <p class="whitespace-pre-wrap break-words text-sm">{{ reply.content }}</p>
            <div class="mt-3 flex flex-wrap items-center gap-3 text-xs text-gray-500">
              <time>{{ formatDateTime(reply.created_at) }}</time>
              <span :class="reply.delivery_status === 'failed' ? 'text-red-500' : ''">{{ t(`affiliate.cash.delivery.${reply.delivery_status}`) }}</span>
              <button v-if="admin && (reply.delivery_status === 'failed' || reply.delivery_status === 'unknown')" type="button" class="btn btn-secondary btn-sm" :disabled="sending" @click="retry(reply)">{{ t('affiliate.cash.retryMail') }}</button>
            </div>
          </article>
        </div>
        <form v-if="admin" class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-700" @submit.prevent="send">
          <p class="text-sm text-gray-500">{{ t('affiliate.cash.adminHint') }}</p>
          <label class="block space-y-2 text-sm"><span>{{ t('affiliate.cash.decision') }}</span>
            <select v-model="decision" class="input" :disabled="sending" data-testid="cash-decision">
              <option value="pending">{{ t('affiliate.cash.replyOnly') }}</option>
              <option v-if="detail.status === 'pending'" value="paid">{{ t('affiliate.cash.markPaid') }}</option>
              <option v-if="detail.status === 'pending'" value="rejected">{{ t('affiliate.cash.reject') }}</option>
            </select>
          </label>
          <p v-if="decision === 'paid'" class="text-sm text-amber-600 dark:text-amber-400">{{ t('affiliate.cash.paidHint') }}</p>
          <p v-if="decision === 'rejected'" class="text-sm text-amber-600 dark:text-amber-400">{{ t('affiliate.cash.rejectHint') }}</p>
          <label class="block space-y-2 text-sm"><span>{{ t('affiliate.cash.replyContent') }}</span><textarea v-model="content" class="input min-h-28" maxlength="4000" required :disabled="sending" data-testid="cash-reply-content" /></label>
          <label v-if="decision !== 'pending'" class="flex items-start gap-2 text-sm"><input v-model="confirmed" type="checkbox" class="mt-1" :disabled="sending" required /><span>{{ t(decision === 'paid' ? 'affiliate.cash.confirmPaid' : 'affiliate.cash.confirmReject') }}</span></label>
          <button type="submit" class="btn btn-primary" :disabled="sending || !content.trim() || (decision !== 'pending' && !confirmed)">{{ t(sending ? 'affiliate.cash.sending' : 'affiliate.cash.sendReply') }}</button>
        </form>
      </div>
      <p v-if="detailError" role="alert" class="mt-4 text-sm text-red-600 dark:text-red-400">{{ detailError }}</p>
    </BaseDialog>
    <BaseDialog :show="uncertainReply !== null" :title="t('affiliate.cash.retryMail')" width="narrow" @close="uncertainReply = null">
      <p class="text-sm">{{ t('affiliate.cash.uncertainHint') }}</p>
      <template #footer><button class="btn btn-secondary" @click="uncertainReply = null">{{ t('common.cancel') }}</button><button class="btn btn-primary" @click="retryUncertain">{{ t('affiliate.cash.confirmRetry') }}</button></template>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { affiliateCashAPI, formatCashAmount } from '@/api/affiliateCash'
import type { CashApplication, CashReply, CashStatus } from '@/api/affiliateCash'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'

const props = withDefaults(defineProps<{ admin?: boolean; refreshKey?: number }>(), { admin: false, refreshKey: 0 })
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const statuses: CashStatus[] = ['pending', 'paid', 'rejected']
const items = ref<CashApplication[]>([])
const page = ref(1), total = ref(0), status = ref(''), loading = ref(false), error = ref('')
const showDetail = ref(false), detailLoading = ref(false), detail = ref<CashApplication | null>(null)
const qrURL = ref(''), qrError = ref(false), detailError = ref(''), sending = ref(false)
const content = ref(''), decision = ref<CashStatus>('pending'), confirmed = ref(false)
const uncertainReply = ref<CashReply | null>(null)
let detailSequence = 0, listSequence = 0
let replyKey = '', replyFingerprint = ''
const statusClass = (value: CashStatus) => ({ pending: 'bg-amber-500/10 text-amber-600 dark:text-amber-400', paid: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400', rejected: 'bg-red-500/10 text-red-600 dark:text-red-400' })[value]

async function load() {
  const sequence = ++listSequence
  loading.value = true; error.value = ''
  try {
    const data = await affiliateCashAPI.list(props.admin, page.value, status.value)
    if (sequence === listSequence) { items.value = data.items; total.value = data.total }
  } catch (err) { if (sequence === listSequence) error.value = extractApiErrorMessage(err, t('affiliate.cash.loadFailed')) }
  finally { if (sequence === listSequence) loading.value = false }
}
function clearQR() { if (qrURL.value) URL.revokeObjectURL(qrURL.value); qrURL.value = '' }
async function loadQR(id: number) {
  const sequence = detailSequence
  qrError.value = false
  try {
    const blob = await affiliateCashAPI.qr(id, props.admin)
    if (sequence === detailSequence && showDetail.value) { clearQR(); qrURL.value = URL.createObjectURL(blob) }
  } catch { if (sequence === detailSequence) qrError.value = true }
}
async function open(id: number) {
  const sequence = ++detailSequence
  clearQR(); detail.value = null; showDetail.value = true; detailLoading.value = true; detailError.value = ''
  content.value = ''; decision.value = 'pending'; confirmed.value = false; replyKey = ''; replyFingerprint = ''
  void loadQR(id)
  try { const data = await affiliateCashAPI.get(id, props.admin); if (sequence === detailSequence) detail.value = data }
  catch (err) { if (sequence === detailSequence) detailError.value = extractApiErrorMessage(err, t('affiliate.cash.loadFailed')) }
  finally { if (sequence === detailSequence) detailLoading.value = false }
}
function close() { if (sending.value) return; ++detailSequence; showDetail.value = false; clearQR() }
function saved(data: CashApplication, replyID?: number) {
  detail.value = data
  const last = replyID ? data.replies?.find(reply => reply.id === replyID) : data.replies?.[data.replies.length - 1]
  if (last?.delivery_status === 'sent') appStore.showSuccess(t('affiliate.cash.replySent'))
  else appStore.showError(t('affiliate.cash.replySavedMailFailed'))
  void load(); emit('changed')
}
async function send() {
  if (!detail.value || sending.value || !content.value.trim() || (decision.value !== 'pending' && !confirmed.value)) return
  const fingerprint = JSON.stringify([detail.value.id, content.value.trim(), decision.value])
  if (fingerprint !== replyFingerprint) { replyKey = crypto.randomUUID(); replyFingerprint = fingerprint }
  sending.value = true; detailError.value = ''
  try {
    saved(await affiliateCashAPI.reply(detail.value.id, content.value.trim(), decision.value, replyKey))
    content.value = ''; decision.value = 'pending'; confirmed.value = false; replyKey = ''; replyFingerprint = ''
  } catch (err) { detailError.value = extractApiErrorMessage(err, t('affiliate.cash.sendFailed')) }
  finally { sending.value = false }
}
async function retry(reply: CashReply, confirmUncertain = false) {
  if (!detail.value || sending.value) return
  if (reply.delivery_status === 'unknown' && !confirmUncertain) { uncertainReply.value = reply; return }
  sending.value = true; detailError.value = ''
  try { saved(await affiliateCashAPI.retry(detail.value.id, reply.id, confirmUncertain), reply.id) }
  catch (err) { detailError.value = extractApiErrorMessage(err, t('affiliate.cash.sendFailed')) }
  finally { sending.value = false }
}
function retryUncertain() { const reply = uncertainReply.value; uncertainReply.value = null; if (reply) void retry(reply, true) }
watch(() => props.refreshKey, () => { page.value = 1; void load() })
watch(decision, () => { confirmed.value = false })
onMounted(load)
onUnmounted(() => { ++detailSequence; ++listSequence; clearQR() })
</script>
