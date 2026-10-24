<template>
  <button v-if="authStore.isAdmin" type="button" class="inline-flex items-center gap-1.5 rounded-lg bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-800 dark:text-dark-400" :aria-label="t('version.title')" @click="openDialog">
    {{ displayVersion }}
    <span v-if="info?.has_update" class="h-1.5 w-1.5 rounded-full bg-emerald-500" :title="t('version.available')" />
  </button>
  <span v-else-if="version" class="inline-flex rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-800 dark:text-dark-400">{{ displayVersion }}</span>

  <BaseDialog :show="show && authStore.isAdmin" :title="t('version.title')" :show-close-button="!updating && !restarting" :close-on-escape="!updating && !restarting" @close="closeDialog">
    <div class="space-y-5">
      <div class="flex items-start justify-between gap-4">
        <div>
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('version.current') }}</p>
          <p class="mt-1 text-2xl font-semibold text-gray-900 dark:text-gray-100">{{ displayVersion }}</p>
          <p v-if="info" class="mt-1 break-all text-xs text-gray-500 dark:text-dark-400">{{ info.repository }}</p>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading || updating || restarting" @click="refresh(true)">{{ loading ? t('version.checking') : t('version.check') }}</button>
      </div>
      <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-3 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p>
      <p v-if="updating || restarting" role="status" class="rounded-xl bg-primary-50 p-3 text-sm text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ restarting ? t('version.restarting') : t('version.installing') }}</p>
      <div v-if="pendingVersion" class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 dark:border-emerald-900 dark:bg-emerald-900/10">
        <p class="text-sm font-medium text-emerald-700 dark:text-emerald-300">{{ t('version.ready', { version: pendingVersion }) }}</p>
        <p class="mt-1 text-xs text-emerald-600 dark:text-emerald-400">{{ t('version.restartHint') }}</p>
        <button type="button" class="btn btn-primary mt-3" :disabled="restarting" @click="restart">{{ t('version.restart') }}</button>
      </div>
      <template v-else-if="info">
        <p v-if="!info.has_update && !error" class="text-sm text-gray-600 dark:text-dark-300">{{ t('version.latest') }}</p>
        <template v-if="info.has_update">
          <label for="release-version" class="block text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('version.select') }}</label>
          <Select id="release-version" v-model="selectedVersion" :options="releaseOptions" :disabled="updating || restarting" />
          <div v-if="selectedRelease" class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <div class="flex items-center justify-between gap-3">
              <h4 class="font-medium text-gray-900 dark:text-gray-100">{{ selectedRelease.name || `v${selectedRelease.version}` }}</h4>
              <a :href="selectedRelease.url" target="_blank" rel="noopener noreferrer" class="text-xs text-primary-600 dark:text-primary-400">{{ t('version.releasePage') }}</a>
            </div>
            <p class="mt-3 max-h-60 overflow-y-auto whitespace-pre-wrap break-words text-sm leading-relaxed text-gray-600 dark:text-dark-300">{{ selectedRelease.notes || t('version.noNotes') }}</p>
          </div>
          <p v-if="!info.can_update" class="text-sm text-amber-600 dark:text-amber-400">{{ t('version.unsupported') }}</p>
          <p v-else-if="selectedRelease && !selectedRelease.can_install" class="text-sm text-amber-600 dark:text-amber-400">{{ t('version.noPackage') }}</p>
          <p v-else class="text-xs leading-relaxed text-gray-500 dark:text-dark-400">{{ t('version.optional') }}</p>
          <div v-if="confirming" class="rounded-xl bg-amber-50 p-4 dark:bg-amber-900/10">
            <p class="text-sm text-amber-800 dark:text-amber-300">{{ t('version.confirm', { version: selectedVersion }) }}</p>
            <div class="mt-3 flex justify-end gap-2">
              <button type="button" class="btn btn-secondary" @click="confirming = false">{{ t('common.cancel') }}</button>
              <button type="button" class="btn btn-primary" :disabled="updating" @click="install">{{ t('version.confirmInstall') }}</button>
            </div>
          </div>
        </template>
      </template>
    </div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="updating || restarting" @click="closeDialog">{{ t('version.later') }}</button>
        <button v-if="!pendingVersion && info?.has_update && !confirming" type="button" class="btn btn-primary" :disabled="!info.can_update || !selectedRelease?.can_install || loading || updating" @click="confirming = true">{{ t('version.install') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { checkUpdates, createSystemOperationKey, getVersion, performUpdate, restartService, type ReleaseUpdateInfo } from '@/api/admin/system'
import BaseDialog from './BaseDialog.vue'
import Select from './Select.vue'

const props = defineProps<{ version?: string }>()
const authStore = useAuthStore()
const { t } = useI18n()
const show = ref(false)
const loading = ref(false)
const updating = ref(false)
const restarting = ref(false)
const confirming = ref(false)
const error = ref('')
const info = ref<ReleaseUpdateInfo | null>(null)
const selectedVersion = ref('')
const installedVersion = ref('')
const displayVersion = computed(() => `v${(info.value?.current_version || props.version || '--').replace(/^v/, '')}`)
const releaseOptions = computed(() => (info.value?.releases || []).map(release => ({ value: release.version, label: `v${release.version}` })))
const selectedRelease = computed(() => info.value?.releases?.find(release => release.version === selectedVersion.value))
const pendingVersion = computed(() => installedVersion.value || info.value?.pending_version || '')
let timer: ReturnType<typeof setInterval> | undefined
let disposed = false
let updateKey = ''
watch(selectedVersion, () => { confirming.value = false; updateKey = '' })

async function refresh(force = false) {
  if (!authStore.isAdmin || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const result = await checkUpdates(force)
    if (disposed || !authStore.isAdmin) return
    info.value = result
    if (!result.releases?.some(release => release.version === selectedVersion.value)) selectedVersion.value = result.releases?.[0]?.version || ''
  } catch {
    error.value = t('version.checkFailed')
  } finally {
    loading.value = false
  }
}

function openDialog() {
  show.value = true
  if (!info.value || error.value) void refresh()
}
function closeDialog() {
  if (updating.value || restarting.value) return
  show.value = false
  confirming.value = false
}
async function install() {
  if (!confirming.value || updating.value || !info.value?.can_update || !selectedRelease.value?.can_install) return
  const target = selectedVersion.value
  updating.value = true
  confirming.value = false
  error.value = ''
  try {
    if (!updateKey) updateKey = createSystemOperationKey('update')
    const result = await performUpdate(target, updateKey)
    installedVersion.value = result.version
  } catch {
    await refresh()
    if (!pendingVersion.value) error.value = t('version.installFailed')
  } finally {
    updating.value = false
  }
}
async function restart() {
  if (restarting.value || !pendingVersion.value) return
  restarting.value = true
  error.value = ''
  try {
    await restartService()
    const deadline = Date.now() + 90 * 1000
    while (Date.now() < deadline && !disposed) {
      await new Promise(resolve => setTimeout(resolve, 2000))
      if (disposed) return
      try {
        const result = await getVersion()
        if (result.version.replace(/^v/, '') === pendingVersion.value.replace(/^v/, '')) { window.location.reload(); return }
      } catch { /* The service can be briefly unavailable while restarting. */ }
    }
    if (!disposed) error.value = t('version.restartTimeout')
  } catch {
    error.value = t('version.restartTimeout')
  } finally {
    restarting.value = false
  }
}

watch(() => authStore.isAdmin, enabled => {
  if (timer) clearInterval(timer)
  if (enabled) {
    void refresh()
    timer = setInterval(() => { if (!updating.value && !restarting.value) void refresh() }, 30 * 60 * 1000)
  } else { show.value = false; info.value = null; installedVersion.value = ''; confirming.value = false; updateKey = '' }
}, { immediate: true })
onBeforeUnmount(() => { disposed = true; if (timer) clearInterval(timer) })
</script>
