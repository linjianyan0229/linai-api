<template>
  <section class="rounded-2xl border border-gray-200 p-4 dark:border-dark-600" data-testid="group-recharge-rate-editor">
    <div class="flex items-center justify-between gap-4">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.groups.rechargeRates.title') }}</h3>
        <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.rechargeRates.hint') }}</p>
      </div>
      <Toggle :model-value="modelValue.length > 0" :aria-label="t('admin.groups.rechargeRates.title')" @update:model-value="toggle" />
    </div>
    <template v-if="modelValue.length > 0">
      <div class="mt-4 grid gap-3 sm:grid-cols-3">
        <div v-for="(tier, index) in modelValue" :key="index" class="min-w-0 space-y-3 rounded-xl bg-gray-50 p-3 dark:bg-dark-800">
          <p class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('admin.groups.rechargeRates.level', { level: index + 1 }) }}</p>
          <label class="block">
            <span class="input-label text-xs">{{ t('admin.groups.rechargeRates.threshold') }}</span>
            <input :value="tier.min_amount" type="number" min="0" max="1000000000000" step="0.01" required :readonly="index === 0" class="input" :data-testid="`recharge-threshold-${index}`" @input="update(index, 'min_amount', $event)" />
          </label>
          <label class="block">
            <span class="input-label text-xs">{{ t('admin.groups.rechargeRates.rate') }}</span>
            <input :value="tier.rate_multiplier" type="number" min="0.5" max="1" step="0.0001" required class="input" :data-testid="`recharge-rate-${index}`" @input="update(index, 'rate_multiplier', $event)" />
          </label>
        </div>
      </div>
      <p v-if="!validGroupRechargeRateTiers(modelValue)" role="alert" class="mt-3 text-xs text-red-600 dark:text-red-400">{{ t('admin.groups.rechargeRates.invalid') }}</p>
      <p class="mt-3 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.rechargeRates.priority') }}</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import type { GroupRechargeRateTier } from '@/types'
import { defaultGroupRechargeRateTiers, validGroupRechargeRateTiers } from '@/utils/groupRechargeRates'

const props = defineProps<{ modelValue: GroupRechargeRateTier[] }>()
const emit = defineEmits<{ 'update:modelValue': [value: GroupRechargeRateTier[]] }>()
const { t } = useI18n()

function toggle(enabled: boolean) {
  emit('update:modelValue', enabled ? defaultGroupRechargeRateTiers() : [])
}

function update(index: number, field: keyof GroupRechargeRateTier, event: Event) {
  const value = (event.target as HTMLInputElement).valueAsNumber
  emit('update:modelValue', props.modelValue.map((tier, i) => i === index ? { ...tier, [field]: value } : { ...tier }))
}
</script>
