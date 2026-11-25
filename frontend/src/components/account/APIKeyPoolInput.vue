<template>
  <div data-testid="api-key-pool">
    <div class="mb-2 flex items-center justify-between gap-3">
      <label class="input-label mb-0">{{ t('admin.accounts.apiKeyPool.label') }}{{ required ? ' *' : '' }}</label>
      <button v-if="!bulkInput || inputCount <= 1" type="button" class="text-xs text-primary-600 hover:text-primary-500" @click="bulkInput = !bulkInput">
        {{ t(bulkInput ? 'admin.accounts.apiKeyPool.singleInput' : 'admin.accounts.apiKeyPool.bulkInput') }}
      </button>
    </div>
    <textarea
      v-if="bulkInput"
      :value="modelValue"
      :required="required"
      :placeholder="t('admin.accounts.apiKeyPool.placeholder')"
      :class="['input font-mono', { 'key-pool-masked': !visible }]"
      rows="4"
      autocomplete="new-password"
      autocapitalize="off"
      :spellcheck="false"
      data-testid="api-key-pool-textarea"
      data-1p-ignore
      data-lpignore="true"
      data-bwignore="true"
      @input="emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
    />
    <input
      v-else
      :value="modelValue"
      :type="visible ? 'text' : 'password'"
      :required="required"
      :placeholder="placeholder"
      class="input font-mono"
      autocomplete="new-password"
      data-testid="api-key-pool-single"
      data-1p-ignore
      data-lpignore="true"
      data-bwignore="true"
      @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)"
    />
    <div class="mt-1 flex items-start justify-between gap-3">
      <p class="input-hint mt-0">{{ t('admin.accounts.apiKeyPool.hint') }}</p>
      <button type="button" class="shrink-0 text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400" @click="visible = !visible">
        {{ t(visible ? 'admin.accounts.apiKeyPool.hide' : 'admin.accounts.apiKeyPool.show') }}
      </button>
    </div>
    <p v-if="editing" class="input-hint" data-testid="api-key-pool-stored-count">
      {{ t('admin.accounts.apiKeyPool.storedCount', { count: storedCount }) }}
      {{ t('admin.accounts.apiKeyPool.keepExisting') }}
    </p>
    <p v-if="inputCount > 1" class="input-hint" data-testid="api-key-pool-input-count">
      {{ t('admin.accounts.apiKeyPool.inputCount', { count: inputCount }) }}
    </p>
    <div v-if="editing && inputCount > 0" class="mt-3">
      <label class="input-label" for="api-key-pool-write-mode">{{ t('admin.accounts.apiKeyPool.writeMode') }}</label>
      <select
        id="api-key-pool-write-mode"
        :value="mode"
        class="input"
        data-testid="api-key-pool-write-mode"
        @change="emit('update:mode', ($event.target as HTMLSelectElement).value as APIKeyWriteMode)"
      >
        <option value="append">{{ t('admin.accounts.apiKeyPool.append') }}</option>
        <option value="replace">{{ t('admin.accounts.apiKeyPool.replace') }}</option>
      </select>
      <p v-if="mode === 'replace'" class="mt-2 text-xs text-amber-600 dark:text-amber-400">
        {{ t('admin.accounts.apiKeyPool.replaceHint') }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { parseAPIKeyInput, type APIKeyWriteMode } from './apiKeyPool'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder?: string
  required?: boolean
  editing?: boolean
  storedCount?: number
  mode?: APIKeyWriteMode
}>(), { placeholder: 'sk-...', required: false, editing: false, storedCount: 0, mode: 'append' })
const emit = defineEmits<{
  'update:modelValue': [value: string]
  'update:mode': [value: APIKeyWriteMode]
}>()
const { t } = useI18n()
const bulkInput = ref(false)
const visible = ref(false)
const inputCount = computed(() => parseAPIKeyInput(props.modelValue).length)
watch(() => props.modelValue, value => {
  if (value.includes('\n')) bulkInput.value = true
  if (!value) visible.value = false
})
</script>

<style scoped>
.key-pool-masked {
  -webkit-text-security: disc;
}
</style>
