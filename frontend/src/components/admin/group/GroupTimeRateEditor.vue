<template>
  <section class="rounded-2xl border border-gray-200 p-4 dark:border-dark-600" data-testid="group-time-rate-editor">
    <div class="flex items-center justify-between gap-4">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.groups.timeRates.title') }}</h3>
        <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.timeRates.hint', { timezone }) }}</p>
      </div>
      <Toggle :model-value="modelValue.length > 0" :aria-label="t('admin.groups.timeRates.title')" @update:model-value="toggle" />
    </div>
    <p v-if="legacyEnabled && !modelValue.length" class="mt-3 text-xs text-amber-600 dark:text-amber-400">{{ t('admin.groups.timeRates.legacy') }}</p>
    <template v-if="modelValue.length === 2">
      <div class="mt-4 grid gap-3 sm:grid-cols-2">
        <div v-for="(period, index) in modelValue" :key="index" class="min-w-0 space-y-3 rounded-xl bg-gray-50 p-3 dark:bg-dark-800">
          <h4 class="text-sm font-semibold">{{ index === 0 ? t('admin.groups.timeRates.peak') : t('admin.groups.timeRates.valley') }}</h4>
          <div class="grid grid-cols-2 gap-2">
            <label><span class="input-label text-xs">{{ t('admin.groups.timeRates.start') }}</span><input :value="period.start" type="time" required class="input min-w-0" :data-testid="`time-start-${index}`" @input="updateTime(index, 'start', $event)" /></label>
            <label><span class="input-label text-xs">{{ t('admin.groups.timeRates.end') }}</span><input :value="period.end" type="time" required class="input min-w-0" :data-testid="`time-end-${index}`" @input="updateTime(index, 'end', $event)" /></label>
          </div>
          <label class="block"><span class="input-label text-xs">{{ rechargeTiers?.length === 3 ? t('admin.groups.timeRates.coefficient') : t('admin.groups.timeRates.rate') }}</span><input :value="period.rate_multiplier" type="number" min="0" max="999999.9999" step="0.0001" required class="input" :data-testid="`time-rate-${index}`" @input="updateRate(index, $event)" /></label>
        </div>
      </div>
      <p v-if="!validGroupTimeRates(modelValue)" role="alert" class="mt-3 text-xs text-red-600 dark:text-red-400">{{ t('admin.groups.timeRates.invalid') }}</p>
      <p class="mt-3 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ rechargeTiers?.length === 3 ? t('admin.groups.timeRates.stacked') : t('admin.groups.timeRates.absolute') }}</p>
      <div v-if="rechargeTiers?.length === 3" class="mt-3 grid gap-2 text-xs sm:grid-cols-3">
        <p v-for="(tier, index) in rechargeTiers" :key="index" class="rounded-xl bg-primary-500/10 p-3 text-primary-700 dark:text-primary-300">{{ t('admin.groups.timeRates.preview', { level: index + 1, peak: product(tier.rate_multiplier, modelValue[0]!.rate_multiplier), valley: product(tier.rate_multiplier, modelValue[1]!.rate_multiplier) }) }}</p>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import type { GroupRechargeRateTier, GroupTimeRatePeriod } from '@/types'
import { defaultGroupTimeRates, validGroupTimeRates } from '@/utils/groupTimeRates'
const props = defineProps<{ modelValue: GroupTimeRatePeriod[]; rechargeTiers?: GroupRechargeRateTier[]; baseRate: number; timezone: string; legacyEnabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [periods: GroupTimeRatePeriod[]] }>()
const { t } = useI18n()
const product = (a: number, b: number) => Number((a * b).toFixed(8))
function toggle(enabled: boolean) {
  emit('update:modelValue', enabled ? defaultGroupTimeRates(props.rechargeTiers?.length === 3 ? 1 : props.baseRate) : [])
}
function updateTime(index: number, field: 'start' | 'end', event: Event) {
  const periods = props.modelValue.map(p => ({ ...p }))
  const value = (event.target as HTMLInputElement).value
  periods[index]![field] = value
  periods[1 - index]![field === 'start' ? 'end' : 'start'] = value
  emit('update:modelValue', periods)
}
function updateRate(index: number, event: Event) {
  emit('update:modelValue', props.modelValue.map((p, i) => i === index ? { ...p, rate_multiplier: (event.target as HTMLInputElement).valueAsNumber } : { ...p }))
}
</script>
