<template>
  <section class="rounded-2xl border border-gray-200 p-4 dark:border-dark-600" data-testid="group-time-rate-editor">
    <div class="flex items-center justify-between gap-4">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.groups.timeRates.title') }}</h3>
        <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.timeRates.hint') }}</p>
      </div>
      <Toggle :model-value="modelValue.length > 0" :aria-label="t('admin.groups.timeRates.title')" @update:model-value="toggle" />
    </div>
    <p v-if="legacyEnabled && !modelValue.length" class="mt-3 text-xs text-amber-600 dark:text-amber-400">{{ t('admin.groups.timeRates.legacy') }}</p>
    <template v-if="modelValue.length">
      <p class="mt-3 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.timeRates.editHint') }}</p>
      <div class="mt-4 grid gap-4 sm:grid-cols-2">
        <div v-for="kind in kinds" :key="kind" class="min-w-0 space-y-3">
          <div class="flex items-center justify-between gap-2">
            <h4 class="text-sm font-semibold">{{ t(`admin.groups.timeRates.${kind}`) }}</h4>
            <button type="button" class="btn btn-secondary btn-sm gap-1" :data-testid="`add-time-${kind}`" :disabled="!additions[kind]" @click="add(kind)">
              <Icon name="plus" size="sm" />{{ t(kind === 'peak' ? 'admin.groups.timeRates.addPeak' : 'admin.groups.timeRates.addValley') }}
            </button>
          </div>
          <div v-for="(row, number) in rows[kind]" :key="row.index" class="min-w-0 space-y-3 rounded-xl bg-gray-50 p-3 dark:bg-dark-800" :data-testid="`time-period-${row.index}`">
            <div class="flex items-center justify-between gap-2">
              <span class="text-xs font-medium">{{ t('admin.groups.timeRates.periodName', { name: t(`admin.groups.timeRates.${kind}`), index: number + 1 }) }}</span>
              <button type="button" class="btn-icon h-7 w-7 text-gray-400 hover:text-red-500" :data-testid="`remove-time-${row.index}`" :aria-label="t('admin.groups.timeRates.removePeriod', { name: t(`admin.groups.timeRates.${kind}`), index: number + 1 })" :disabled="modelValue.length <= 2 || !validGroupTimeRates(modelValue)" @click="remove(row.index)"><Icon name="trash" size="sm" /></button>
            </div>
            <div class="grid grid-cols-2 gap-2">
              <label><span class="input-label text-xs">{{ t('admin.groups.timeRates.start') }}</span><input :value="row.period.start" type="time" required class="input min-w-0" :data-testid="`time-start-${row.index}`" @input="updateTime(row.index, 'start', $event)" /></label>
              <label><span class="input-label text-xs">{{ t('admin.groups.timeRates.end') }}</span><input :value="row.period.end" type="time" required class="input min-w-0" :data-testid="`time-end-${row.index}`" @input="updateTime(row.index, 'end', $event)" /></label>
            </div>
            <label class="block"><span class="input-label text-xs">{{ rechargeTiers?.length === 3 ? t('admin.groups.timeRates.coefficient') : t('admin.groups.timeRates.rate') }}</span><input :value="row.period.rate_multiplier" type="number" min="0" max="999999.9999" step="0.0001" required class="input" :data-testid="`time-rate-${row.index}`" @input="updateRate(row.index, $event)" /></label>
            <div v-if="rechargeTiers?.length === 3" class="flex flex-wrap gap-x-3 gap-y-1 text-xs text-primary-700 dark:text-primary-300">
              <span v-for="(tier, level) in rechargeTiers" :key="level">{{ t('admin.groups.timeRates.tierPreview', { level: level + 1, rate: product(tier.rate_multiplier, row.period.rate_multiplier) }) }}</span>
            </div>
          </div>
          <p v-if="!rows[kind].length" class="text-xs text-gray-400">{{ t('admin.groups.timeRates.noPeriods') }}</p>
        </div>
      </div>
      <p v-if="!validGroupTimeRates(modelValue)" role="alert" class="mt-3 text-xs text-red-600 dark:text-red-400">{{ t('admin.groups.timeRates.invalid') }}</p>
      <p class="mt-3 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ rechargeTiers?.length === 3 ? t('admin.groups.timeRates.stacked') : t('admin.groups.timeRates.absolute') }}</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import type { GroupRechargeRateTier, GroupTimeRatePeriod } from '@/types'
import { addGroupTimeRate, defaultGroupTimeRates, groupTimeRateKind, normalizeGroupTimeRates, removeGroupTimeRate, validGroupTimeRates, type GroupTimeRateKind } from '@/utils/groupTimeRates'
const props = defineProps<{ modelValue: GroupTimeRatePeriod[]; rechargeTiers?: GroupRechargeRateTier[]; baseRate: number; legacyEnabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [periods: GroupTimeRatePeriod[]] }>()
const { t } = useI18n()
const kinds = ['peak', 'valley'] as const
const product = (a: number, b: number) => Number((a * b).toFixed(8))
const rows = computed(() => {
  const all = props.modelValue.map((period, index) => ({ period, index, kind: groupTimeRateKind(period, index) }))
  return { peak: all.filter(row => row.kind === 'peak'), valley: all.filter(row => row.kind === 'valley') }
})
const additions = computed(() => ({ peak: addGroupTimeRate(props.modelValue, 'peak'), valley: addGroupTimeRate(props.modelValue, 'valley') }))
function toggle(enabled: boolean) {
  emit('update:modelValue', enabled ? defaultGroupTimeRates(props.rechargeTiers?.length === 3 ? 1 : props.baseRate) : [])
}
function add(kind: GroupTimeRateKind) {
  const periods = additions.value[kind]
  if (periods) emit('update:modelValue', periods)
}
function remove(index: number) {
  const periods = removeGroupTimeRate(props.modelValue, index)
  if (periods) emit('update:modelValue', periods)
}
function updateTime(index: number, field: 'start' | 'end', event: Event) {
  const periods = normalizeGroupTimeRates(props.modelValue)
  const previous = periods[index]![field]
  const opposite = field === 'start' ? 'end' : 'start'
  const neighbor = periods.find((p, i) => i !== index && p[opposite] === previous)
  const value = (event.target as HTMLInputElement).value
  periods[index]![field] = value
  if (neighbor) neighbor[opposite] = value
  emit('update:modelValue', periods)
}
function updateRate(index: number, event: Event) {
  emit('update:modelValue', normalizeGroupTimeRates(props.modelValue).map((p, i) => i === index ? { ...p, rate_multiplier: (event.target as HTMLInputElement).valueAsNumber } : p))
}
</script>
