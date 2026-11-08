<template>
  <section class="rounded-2xl border border-gray-200 p-4 dark:border-dark-600" data-testid="group-dynamic-rate-editor">
    <div class="flex items-start justify-between gap-4">
      <div class="min-w-0">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.groups.dynamicRates.title') }}</h3>
        <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.dynamicRates.hint') }}</p>
      </div>
      <Toggle :model-value="enabled" :disabled="!enabled && !canEnable" :aria-label="t('admin.groups.dynamicRates.title')" @update:model-value="emit('update:enabled', $event)" />
    </div>
    <p v-if="!groupId" class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.groups.dynamicRates.createFirst') }}</p>
    <template v-else>
      <div class="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs">
        <span :class="canEnable ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'" role="status">{{ loading ? t('common.loading') : error ? t('admin.groups.dynamicRates.loadFailed') : canEnable ? t('admin.groups.dynamicRates.sources', { count: availability?.eligible_accounts }) : t('admin.groups.dynamicRates.unavailable') }}</span>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="refresh">{{ t('common.refresh') }}</button>
      </div>
      <p v-if="!canEnable && !loading" class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.dynamicRates.setupHint') }}</p>
    </template>
    <template v-if="enabled">
      <label class="mt-4 block">
        <span class="input-label">{{ t('admin.groups.dynamicRates.factor') }}</span>
        <input :value="factor" type="number" min="0.0001" max="9999.9999" step="0.0001" required class="input" data-testid="dynamic-rate-factor" @input="emit('update:factor', Number(($event.target as HTMLInputElement).value))" />
      </label>
      <p v-if="!validDynamicRateFactor(factor)" role="alert" class="mt-2 text-xs text-red-600">{{ t('admin.groups.dynamicRates.invalid') }}</p>
      <div v-if="preview" class="mt-3 rounded-xl bg-gray-50 p-3 text-sm dark:bg-dark-800" data-testid="dynamic-rate-preview">{{ t('admin.groups.dynamicRates.preview', { upstream: upstreamRange, factor, result: preview }) }}</div>
      <p class="mt-3 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.dynamicRates.paused') }}</p>
      <p class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.dynamicRates.freshness') }}</p>
      <p class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('admin.groups.dynamicRates.costHint') }}</p>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { getDynamicRateAvailability, type GroupDynamicRateAvailability } from '@/api/admin/groups'
import { dynamicRateRange, validDynamicRateFactor } from '@/utils/groupDynamicRates'

const props = defineProps<{ enabled: boolean; factor: number; groupId?: number }>()
const emit = defineEmits<{ 'update:enabled': [value: boolean]; 'update:factor': [value: number] }>()
const { t } = useI18n()
const availability = ref<GroupDynamicRateAvailability | null>(null)
const loading = ref(false), error = ref(false)
let controller: AbortController | undefined
const canEnable = computed(() => (availability.value?.eligible_accounts ?? 0) > 0 && !error.value)
const upstreamRange = computed(() => dynamicRateRange({ dynamic_rate_min: availability.value?.upstream_min, dynamic_rate_max: availability.value?.upstream_max }))
const preview = computed(() => validDynamicRateFactor(props.factor) && availability.value?.upstream_min != null && availability.value?.upstream_max != null ? dynamicRateRange({ dynamic_rate_min: availability.value.upstream_min * props.factor, dynamic_rate_max: availability.value.upstream_max * props.factor }) : null)

async function refresh() {
  controller?.abort()
  const id = props.groupId
  if (!id) { availability.value = null; return }
  const request = new AbortController()
  controller = request
  loading.value = true
  error.value = false
  try {
    const result = await getDynamicRateAvailability(id, request.signal)
    if (!request.signal.aborted && props.groupId === id) availability.value = result
  } catch {
    if (!request.signal.aborted) { availability.value = null; error.value = true }
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
watch(() => props.groupId, () => { availability.value = null; void refresh() }, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
