<template>
  <BaseDialog
    :show="show"
    :title="t('keys.bulkDelete.title')"
    width="narrow"
    :close-on-escape="!submitting"
    :show-close-button="!submitting"
    @close="close"
  >
    <form id="bulk-delete-keys-form" class="space-y-4" @submit.prevent="submit">
      <p class="text-sm text-gray-600 dark:text-gray-400">
        {{ t('keys.bulkDelete.confirmMessage', { count: pendingKeys.length }) }}
      </p>
      <ul class="max-h-40 space-y-1 overflow-y-auto text-sm text-gray-700 dark:text-gray-300">
        <li v-for="key in pendingKeys" :key="key.id" class="break-words">#{{ key.id }} {{ key.name }}</li>
      </ul>
      <div v-if="failures.length" role="alert" class="space-y-2 text-sm text-red-600 dark:text-red-400">
        <p>{{ t('keys.bulkDelete.failureHint') }}</p>
        <ul class="max-h-40 space-y-1 overflow-y-auto">
          <li v-for="failure in failures" :key="failure.id" class="break-words">
            #{{ failure.id }} {{ failure.name }}: {{ failure.message }}
          </li>
        </ul>
      </div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" data-test="cancel" @click="close">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="bulk-delete-keys-form"
          class="btn btn-danger"
          :disabled="submitting || !pendingKeys.length"
          :aria-busy="submitting"
          data-test="submit"
        >
          {{ submitting ? t('keys.bulkDelete.deleting') : t('keys.bulkDelete.confirm', { count: pendingKeys.length }) }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { keysAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

type SelectedKey = Pick<ApiKey, 'id' | 'name'>
const props = defineProps<{ show: boolean; selectedKeys: SelectedKey[] }>()
const emit = defineEmits<{ close: []; deleted: [ids: number[]] }>()
const { t } = useI18n()
const appStore = useAppStore()
const submitting = ref(false)
const pendingKeys = ref<SelectedKey[]>([])
const failures = ref<Array<{ id: number; name: string; message: string }>>([])

watch(() => props.show, (show) => {
  if (!show) return
  // Keep the confirmation tied to the selection the user reviewed.
  pendingKeys.value = props.selectedKeys.map(({ id, name }) => ({ id, name }))
  failures.value = []
}, { immediate: true })

const close = () => {
  if (!submitting.value) emit('close')
}

const errorMessage = (error: unknown): string => {
  const message = (error as { message?: unknown } | null)?.message
  return typeof message === 'string' && message ? message : t('keys.failedToDelete')
}

const submit = async () => {
  if (!props.show || submitting.value || !pendingKeys.value.length) return
  submitting.value = true
  try {
    const result = await keysAPI.bulkDelete(pendingKeys.value.map((key) => key.id))
    failures.value = result.failures.map(({ id, error }) => ({
      id,
      name: pendingKeys.value.find((key) => key.id === id)?.name ?? '',
      message: errorMessage(error)
    }))
    const failedIds = new Set(result.failures.map(({ id }) => id))
    pendingKeys.value = pendingKeys.value.filter((key) => failedIds.has(key.id))
    if (result.succeededIds.length) emit('deleted', result.succeededIds)
    if (result.failures.length) {
      appStore.showError(t('keys.bulkDelete.partialFailure', {
        success: result.succeededIds.length, failed: result.failures.length
      }))
    } else {
      appStore.showSuccess(t('keys.bulkDelete.success', { count: result.succeededIds.length }))
      emit('close')
    }
  } catch (error) {
    appStore.showError(errorMessage(error))
  } finally {
    submitting.value = false
  }
}
</script>
