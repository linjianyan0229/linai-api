<template>
  <div class="space-y-5" data-testid="cash-panel">
    <section class="card p-6">
      <div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-start">
        <div>
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('affiliate.cash.title') }}</h3>
          <p class="mt-2 max-w-3xl text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('affiliate.cash.rules') }}</p>
        </div>
        <button class="btn btn-primary shrink-0" :disabled="!summary || eligible <= 0n || loading" @click="openForm">{{ t('affiliate.cash.apply') }}</button>
      </div>
      <div v-if="summary" class="mt-5 grid gap-4 sm:grid-cols-2">
        <div class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700">
          <p class="text-sm text-gray-500">{{ t('affiliate.cash.eligible') }}</p>
          <p class="mt-2 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">{{ formatCashAmount(summary.eligible_amount) }}</p>
          <p v-if="summary.next_expires_at" class="mt-2 text-xs text-amber-600 dark:text-amber-400">{{ t('affiliate.cash.expiresAt', { time: formatDateTime(summary.next_expires_at) }) }}</p>
          <p v-else class="mt-2 text-xs text-gray-500">{{ t('affiliate.cash.noEligible') }}</p>
        </div>
        <div class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700">
          <p class="text-sm text-gray-500">{{ t('affiliate.cash.reserved') }}</p>
          <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ formatCashAmount(summary.pending_amount) }}</p>
          <p class="mt-2 text-xs leading-5 text-gray-500">{{ t('affiliate.cash.reservedHint') }}</p>
        </div>
      </div>
      <p v-if="loadError" role="alert" class="mt-4 text-sm text-red-600 dark:text-red-400">{{ loadError }} <button class="underline" @click="refresh">{{ t('common.refresh') }}</button></p>
    </section>
    <AffiliateCashApplications :refresh-key="refreshKey" @changed="refreshSummary(); emit('changed')" />
    <BaseDialog :show="showForm" :title="t('affiliate.cash.apply')" :show-close-button="!submitting" :close-on-escape="!submitting" @close="closeForm">
      <form class="space-y-4" @submit.prevent="submit">
        <p class="text-sm leading-6 text-gray-500">{{ t('affiliate.cash.formHint') }}</p>
        <label class="block space-y-2 text-sm"><span>{{ t('affiliate.cash.email') }}</span><input v-model="email" type="email" maxlength="254" required class="input" autocomplete="email" :disabled="submitting" data-testid="cash-email" /></label>
        <label class="block space-y-2 text-sm"><span>{{ t('affiliate.cash.amountUSD') }}</span><input v-model="amount" type="text" inputmode="decimal" pattern="[0-9]{1,10}(\.[0-9]{1,8})?" required class="input" :disabled="submitting" data-testid="cash-amount" /><span class="block text-xs text-gray-500">{{ t('affiliate.cash.availableHint', { amount: formatCashAmount(summary?.eligible_amount || '0') }) }}</span></label>
        <label class="block space-y-2 text-sm"><span>{{ t('affiliate.cash.paymentQR') }}</span><input ref="fileInput" type="file" accept="image/png,image/jpeg,image/webp" required class="input h-auto py-3" :disabled="submitting" data-testid="cash-qr" @change="selectQR" /><span class="block text-xs text-gray-500">{{ t('affiliate.cash.qrHint') }}</span></label>
        <img v-if="qrPreview" :src="qrPreview" :alt="t('affiliate.cash.paymentQR')" class="mx-auto max-h-48 rounded-xl bg-white p-2" />
        <p v-if="formError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ formError }}</p>
        <div class="flex justify-end gap-3 pt-2"><button type="button" class="btn btn-secondary" :disabled="submitting" @click="closeForm">{{ t('common.cancel') }}</button><button type="submit" class="btn btn-primary" :disabled="submitting || !validForm">{{ t(submitting ? 'affiliate.cash.submitting' : 'affiliate.cash.submit') }}</button></div>
      </form>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { affiliateCashAPI, cashAmountUnits, formatCashAmount } from '@/api/affiliateCash'
import type { CashSummary } from '@/api/affiliateCash'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import AffiliateCashApplications from './AffiliateCashApplications.vue'

const { t } = useI18n()
const appStore = useAppStore(), authStore = useAuthStore()
const emit = defineEmits<{ changed: [] }>()
const summary = ref<CashSummary | null>(null), loading = ref(false), loadError = ref(''), refreshKey = ref(0)
const showForm = ref(false), submitting = ref(false), email = ref(''), amount = ref(''), qr = ref<File | null>(null)
const qrPreview = ref(''), fileInput = ref<HTMLInputElement | null>(null), formError = ref('')
const eligible = computed(() => cashAmountUnits(summary.value?.eligible_amount || '0') ?? 0n)
const validForm = computed(() => {
  const units = cashAmountUnits(amount.value.trim())
  return !!email.value.trim() && !!qr.value && units !== null && units > 0n && units <= eligible.value
})
let operationKey = '', fingerprint = '', disposed = false
let refreshTimer: ReturnType<typeof setInterval> | undefined
let expiryTimer: ReturnType<typeof setTimeout> | undefined
let summaryRequest: Promise<void> | null = null

function refreshSummary(): Promise<void> {
  if (summaryRequest) return summaryRequest
  loading.value = true
  summaryRequest = (async () => {
    try {
      const data = await affiliateCashAPI.summary()
      if (disposed) return
      summary.value = data; loadError.value = ''
      if (expiryTimer) clearTimeout(expiryTimer)
      if (data.next_expires_at) {
        const remaining = new Date(data.next_expires_at).getTime() - new Date(data.server_time).getTime()
        expiryTimer = setTimeout(() => { void refreshSummary() }, Math.max(1000, Math.min(remaining + 100, 86400000)))
      }
    } catch (err) { if (!disposed) loadError.value = extractApiErrorMessage(err, t('affiliate.cash.loadFailed')) }
    finally { loading.value = false; summaryRequest = null }
  })()
  return summaryRequest
}
async function refresh() {
  // A read started before a mutation may contain the old quota. Let it finish
  // before requesting a fresh snapshot after a successful application/transfer.
  if (summaryRequest) await summaryRequest
  await refreshSummary()
  refreshKey.value++
}
function clearPreview() { if (qrPreview.value) URL.revokeObjectURL(qrPreview.value); qrPreview.value = '' }
function openForm() {
  email.value = authStore.user?.email || ''; amount.value = ''; qr.value = null; clearPreview()
  formError.value = ''; operationKey = ''; fingerprint = ''; showForm.value = true
}
function closeForm() { if (submitting.value) return; showForm.value = false; clearPreview() }
function selectQR(event: Event) {
  clearPreview(); qr.value = null; formError.value = ''
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > 2 * 1024 * 1024 || !file.size) {
    formError.value = t('affiliate.cash.qrInvalid'); input.value = ''; return
  }
  qr.value = file; qrPreview.value = URL.createObjectURL(file)
}
async function submit() {
  if (!validForm.value || !qr.value || submitting.value) return
  const nextFingerprint = JSON.stringify([email.value.trim(), amount.value.trim(), qr.value.name, qr.value.size, qr.value.lastModified])
  if (fingerprint !== nextFingerprint) { operationKey = crypto.randomUUID(); fingerprint = nextFingerprint }
  submitting.value = true; formError.value = ''
  try {
    await affiliateCashAPI.apply(email.value.trim(), amount.value.trim(), qr.value, operationKey)
    showForm.value = false; clearPreview(); appStore.showSuccess(t('affiliate.cash.applied'))
    await refresh(); emit('changed')
  } catch (err) { formError.value = extractApiErrorMessage(err, t('affiliate.cash.applyFailed')); void refreshSummary() }
  finally { submitting.value = false }
}
defineExpose({ refresh })
onMounted(() => { void refreshSummary(); refreshTimer = setInterval(() => { void refreshSummary() }, 60000); window.addEventListener('focus', refreshSummary) })
onUnmounted(() => { disposed = true; if (refreshTimer) clearInterval(refreshTimer); if (expiryTimer) clearTimeout(expiryTimer); window.removeEventListener('focus', refreshSummary); clearPreview() })
</script>
