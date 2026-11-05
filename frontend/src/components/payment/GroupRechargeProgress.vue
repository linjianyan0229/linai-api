<template>
  <section v-if="error || progress?.groups.length" class="card p-5" data-testid="group-recharge-progress">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('payment.groupRates.title') }}</h2>
        <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('payment.groupRates.hint') }}</p>
      </div>
      <div v-if="progress" class="text-right">
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.groupRates.principal') }}</p>
        <p class="mt-1 text-lg font-semibold tabular-nums text-primary-600 dark:text-primary-400">${{ progress.principal_usd.toFixed(2) }}</p>
      </div>
    </div>
    <div v-if="error" class="mt-4 flex items-center justify-between gap-3 text-sm" role="alert">
      <p class="text-gray-500 dark:text-gray-400">{{ t('payment.groupRates.loadError') }}</p>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="refresh">{{ t('common.tryAgain') }}</button>
    </div>
    <div v-for="group in progress?.groups ?? []" :key="group.group_id" class="mt-5 border-t border-gray-100 pt-4 dark:border-dark-700">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 class="font-medium text-gray-900 dark:text-white">{{ group.group_name }}</h3>
        <span class="text-sm font-semibold text-primary-600 dark:text-primary-400">{{ t('payment.groupRates.current', { level: group.level, rate: group.rate_multiplier }) }}</span>
      </div>
      <ol class="mt-3 grid grid-cols-3 gap-2">
        <li v-for="(tier, index) in group.tiers" :key="index" :aria-current="group.level === index + 1 ? 'step' : undefined"
          class="min-w-0 rounded-xl border p-3"
          :class="group.level === index + 1 ? 'border-primary-400/50 bg-primary-500/10' : 'border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-800'">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('payment.groupRates.level', { level: index + 1 }) }}</p>
          <p class="mt-1 text-lg font-semibold tabular-nums text-gray-900 dark:text-white">{{ tier.rate_multiplier }}×</p>
          <p class="mt-1 break-words text-xs tabular-nums text-gray-500 dark:text-gray-400">≥ ${{ tier.min_amount.toFixed(2) }}</p>
        </li>
      </ol>
      <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
        {{ group.level < 3 ? t('payment.groupRates.next', { amount: Math.max(0, (group.tiers[group.level]?.min_amount ?? 0) - (progress?.principal_usd ?? 0)).toFixed(2) }) : t('payment.groupRates.highest') }}
      </p>
      <p v-if="group.has_user_override" class="mt-1 text-xs text-amber-600 dark:text-amber-400">{{ t('payment.groupRates.override') }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getRechargeProgress, type UserRechargeProgress } from '@/api/groups'

const { t } = useI18n()
const progress = ref<UserRechargeProgress | null>(null)
const loading = ref(false)
const error = ref(false)

async function refresh() {
  if (loading.value) return
  loading.value = true
  error.value = false
  try {
    progress.value = await getRechargeProgress()
  } catch {
    progress.value = null
    error.value = true
  } finally {
    loading.value = false
  }
}

onMounted(refresh)
defineExpose({ refresh })
</script>
