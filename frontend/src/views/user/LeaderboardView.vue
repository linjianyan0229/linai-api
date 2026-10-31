<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <section class="card relative overflow-hidden p-6 sm:p-8">
        <div aria-hidden="true" class="pointer-events-none absolute -right-12 -top-24 h-64 w-64 rounded-full bg-primary-400/10 blur-3xl" />
        <div class="relative flex flex-col gap-6 md:flex-row md:items-end md:justify-between">
          <div class="max-w-2xl">
            <div class="mb-3 inline-flex items-center gap-2 rounded-full border border-primary-200/60 px-3 py-1 text-xs font-semibold tracking-wide text-primary-700 dark:border-primary-700/40 dark:text-primary-300">
              <Icon name="trophy" size="sm" /><span>TOP 20</span>
            </div>
            <h2 class="text-2xl font-semibold tracking-tight text-gray-900 dark:text-white sm:text-3xl">{{ t('leaderboard.title') }}</h2>
            <p class="mt-3 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('leaderboard.description') }}</p>
          </div>
          <div class="flex w-full items-end gap-3 md:w-80 md:shrink-0">
            <div class="min-w-0 flex-1">
              <label for="leaderboard-model" class="input-label">{{ t('leaderboard.model') }}</label>
              <Select id="leaderboard-model" v-model="model" :options="modelOptions" searchable clearable :aria-label="t('leaderboard.model')" :placeholder="t('leaderboard.allModels')" />
            </div>
            <button type="button" class="btn btn-secondary shrink-0" :disabled="loading" :aria-label="t('common.refresh')" @click="load">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>
      </section>

      <p v-if="error" role="alert" class="rounded-2xl border border-red-200 bg-red-50 px-5 py-4 text-sm text-red-700 dark:border-red-900/40 dark:bg-red-950/20 dark:text-red-300">{{ error }}</p>

      <section class="card overflow-hidden" :aria-busy="loading" data-testid="leaderboard-results">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <h3 class="min-w-0 break-all text-sm font-semibold text-gray-800 dark:text-gray-200">{{ model || t('leaderboard.allModels') }}</h3>
          <span v-if="snapshot" class="text-xs text-gray-500 dark:text-dark-400">{{ t('leaderboard.updatedAt', { time: formatDateTime(snapshot.updated_at) }) }}</span>
        </div>
        <DataTable :columns="columns" :data="snapshot?.items ?? []" :loading="loading" row-key="rank">
          <template #cell-rank="{ row }">
            <span class="inline-flex h-9 w-9 items-center justify-center rounded-xl font-semibold tabular-nums" :class="rankClass(row.rank)">{{ row.rank }}</span>
          </template>
          <template #cell-username="{ row }">
            <div class="flex min-w-0 items-center gap-3">
              <span aria-hidden="true" class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-primary-500/10 text-sm font-medium text-primary-700 dark:text-primary-300">{{ displayName(row).slice(0, 1).toUpperCase() }}</span>
              <span class="max-w-44 truncate font-medium text-gray-900 dark:text-white sm:max-w-64" :title="displayName(row)">{{ displayName(row) }}</span>
            </div>
          </template>
          <template #cell-total_tokens="{ row }">
            <div class="space-y-2 py-1">
              <span class="font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatTokens(row.total_tokens) }}</span>
              <div aria-hidden="true" class="ml-auto h-1 w-28 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700"><div class="h-full rounded-full bg-primary-500/70" :style="{ width: `${tokenShare(row.total_tokens)}%` }" /></div>
            </div>
          </template>
          <template #cell-total_quota="{ row }"><span class="whitespace-nowrap font-medium tabular-nums text-gray-700 dark:text-gray-300">{{ formatQuota(row.total_quota) }}</span></template>
          <template #empty><div class="p-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ error ? t('leaderboard.unavailable') : t('leaderboard.empty') }}</div></template>
        </DataTable>
      </section>
      <p class="px-1 text-xs leading-6 text-gray-500 dark:text-dark-400">{{ t('leaderboard.rules') }}</p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import DataTable from '@/components/common/DataTable.vue'
import type { Column } from '@/components/common/types'
import { getLeaderboard, type LeaderboardEntry, type LeaderboardSnapshot } from '@/api/leaderboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const { t, locale } = useI18n()
const model = ref(''), loading = ref(false), error = ref('')
const snapshot = ref<LeaderboardSnapshot | null>(null)
const models = ref<string[]>([])
let sequence = 0
let controller: AbortController | undefined
const modelOptions = computed(() => [{ value: '', label: t('leaderboard.allModels') }, ...models.value.map(value => ({ value, label: value }))])
const columns = computed<Column[]>(() => [
  { key: 'rank', label: t('leaderboard.rank'), class: 'w-24' },
  { key: 'username', label: t('leaderboard.username') },
  { key: 'total_tokens', label: t('leaderboard.totalTokens'), class: 'text-right' },
  { key: 'total_quota', label: t('leaderboard.totalQuota'), class: 'text-right' }
])
const displayName = (row: LeaderboardEntry) => row.username || t('leaderboard.anonymous')
const formatTokens = (value: number) => new Intl.NumberFormat(locale.value).format(value)
const formatQuota = (value: string) => `USD ${new Intl.NumberFormat(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 8 }).format(Number(value))}`
const tokenShare = (value: number) => snapshot.value?.items[0]?.total_tokens ? Math.max(2, Math.min(100, value / snapshot.value.items[0].total_tokens * 100)) : 0
const rankClass = (rank: number) => rank === 1 ? 'bg-amber-400/15 text-amber-700 dark:text-amber-300' : rank === 2 ? 'bg-slate-400/15 text-slate-600 dark:text-slate-300' : rank === 3 ? 'bg-orange-400/15 text-orange-700 dark:text-orange-300' : 'text-gray-400 dark:text-dark-400'

async function load() {
  const request = ++sequence
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  error.value = ''
  snapshot.value = null
  try {
    const result = await getLeaderboard(model.value, controller.signal)
    if (request !== sequence) return
    snapshot.value = result
    models.value = result.models
  } catch (err) {
    if (request === sequence) error.value = extractApiErrorMessage(err, t('leaderboard.loadFailed'))
  } finally { if (request === sequence) loading.value = false }
}

watch(model, load)
onMounted(load)
onBeforeUnmount(() => { ++sequence; controller?.abort() })
</script>
