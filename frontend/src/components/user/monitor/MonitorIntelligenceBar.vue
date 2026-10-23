<template>
  <div class="mt-4 border-t border-gray-100 pt-3 dark:border-dark-700" data-testid="monitor-intelligence">
    <div class="flex items-center justify-between gap-2 text-xs">
      <span class="font-medium text-gray-700 dark:text-gray-200">{{ t('monitorCommon.candy.title') }}</span>
      <span class="font-medium" :class="statusClass">{{ statusLabel }}</span>
    </div>
    <div class="mt-2 flex h-3.5 gap-px" role="img" :aria-label="`${value.model}: ${statusLabel}`">
      <span
        v-for="(point, index) in points"
        :key="index"
        class="min-w-0 flex-1 rounded-sm"
        :class="pointClass(point?.status)"
        :title="point ? pointTitle(point) : t('monitorCommon.candy.noRecord')"
        :data-status="point?.status || 'empty'"
      />
    </div>
    <div class="mt-1.5 flex items-center justify-between gap-2 text-[10px] text-gray-400 dark:text-dark-400">
      <span>{{ t(value.enabled ? 'monitorCommon.candy.interval' : 'monitorCommon.candy.disabledHint') }}</span>
      <span v-if="value.enabled">{{ value.pass_rate == null ? t('monitorCommon.candy.unjudged') : t('monitorCommon.candy.passRate', { rate: value.pass_rate.toFixed(0) }) }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CandyMonitorPoint, CandyMonitorView, CandyStatus } from '@/api/channelMonitor'

const props = defineProps<{ value: CandyMonitorView }>()
const { t, locale } = useI18n()
const points = computed(() => {
  const history = props.value.history.slice(-60)
  return [...Array<null>(Math.max(0, 60 - history.length)).fill(null), ...history]
})
const statusLabel = computed(() => props.value.running
  ? t('monitorCommon.candy.running')
  : t(`monitorCommon.candy.status.${props.value.status}`))
const statusClass = computed(() => {
  if (props.value.running) return 'text-primary-600 dark:text-primary-400'
  if (props.value.status === 'passed') return 'text-emerald-600 dark:text-emerald-400'
  if (props.value.status === 'incorrect') return 'text-amber-600 dark:text-amber-400'
  return 'text-gray-500 dark:text-dark-400'
})

function pointClass(status?: CandyStatus) {
  if (status === 'passed') return 'bg-emerald-500/80 dark:bg-emerald-400/70'
  if (status === 'incorrect') return 'bg-amber-500/85 dark:bg-amber-400/75'
  if (status === 'error') return 'bg-gray-400 dark:bg-dark-500'
  return 'bg-gray-100 dark:bg-dark-800'
}

function pointTitle(point: CandyMonitorPoint) {
  const time = new Date(point.checked_at).toLocaleString(locale.value, {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'
  })
  return `${time} · ${t(`monitorCommon.candy.status.${point.status}`)}`
}
</script>
