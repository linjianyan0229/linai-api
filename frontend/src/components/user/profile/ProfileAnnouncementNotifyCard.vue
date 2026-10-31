<template>
  <section class="card" data-testid="profile-announcement-notify-card" :aria-busy="loading || saving">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-medium text-gray-900 dark:text-white">{{ t('profile.announcementEmail.title') }}</h2>
      <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-gray-400">{{ t('profile.announcementEmail.description') }}</p>
    </div>
    <div class="space-y-4 p-6">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0 space-y-2">
          <p class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('profile.announcementEmail.enabled') }}</p>
          <p class="break-all text-sm text-gray-500 dark:text-gray-400">{{ preference?.available ? t('profile.announcementEmail.recipient', { email: preference.email }) : t('profile.announcementEmail.bindEmail') }}</p>
        </div>
        <Toggle
          :model-value="preference?.enabled ?? false"
          :disabled="loading || saving || !preference || (!preference.available && !preference.enabled)"
          :aria-label="t('profile.announcementEmail.enabled')"
          class="shrink-0"
          @update:model-value="save"
        />
      </div>
      <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('profile.announcementEmail.hint') }}</p>
      <p v-if="saving" role="status" class="text-sm text-primary-600 dark:text-primary-400">{{ t('common.saving') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">
        {{ error }} <button v-if="!preference" type="button" class="underline" @click="load">{{ t('common.refresh') }}</button>
      </p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { getAnnouncementEmailPreference, updateAnnouncementEmailPreference, type AnnouncementEmailPreference } from '@/api/user'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ userEmail: string }>()
const { t } = useI18n()
const appStore = useAppStore()
const preference = ref<AnnouncementEmailPreference | null>(null)
const loading = ref(false), saving = ref(false), error = ref('')
let sequence = 0

async function load() {
  const request = ++sequence
  loading.value = true
  error.value = ''
  try {
    const result = await getAnnouncementEmailPreference()
    if (request === sequence) preference.value = result
  } catch (err) {
    if (request === sequence) error.value = extractApiErrorMessage(err, t('profile.announcementEmail.loadFailed'))
  } finally { if (request === sequence) loading.value = false }
}

async function save(enabled: boolean) {
  if (saving.value || loading.value || !preference.value) return
  const request = ++sequence
  saving.value = true
  error.value = ''
  try {
    const result = await updateAnnouncementEmailPreference(enabled)
    if (request === sequence) {
      preference.value = result
      appStore.showSuccess(t(enabled ? 'profile.announcementEmail.savedEnabled' : 'profile.announcementEmail.savedDisabled'))
    }
  } catch (err) {
    if (request === sequence) error.value = extractApiErrorMessage(err, t('profile.announcementEmail.saveFailed'))
  } finally { saving.value = false }
}

watch(() => props.userEmail, () => { void load() }, { immediate: true })
onBeforeUnmount(() => { ++sequence })
</script>
