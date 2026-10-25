<template>
  <div
    v-if="currentAnnouncement"
    class="announcement-ticker flex min-w-0 items-center gap-1"
    @mouseenter="hovered = true"
    @mouseleave="hovered = false"
    @focusin="focused = true"
    @focusout="handleFocusOut"
  >
    <button
      type="button"
      class="flex h-9 min-w-0 flex-1 items-center gap-2 px-3 text-left"
      data-testid="announcement-ticker-open"
      :aria-label="t('announcements.expandAnnouncement', { title: currentAnnouncement.title })"
      aria-haspopup="dialog"
      @click="emit('open', currentAnnouncement)"
    >
      <span
        v-if="!currentAnnouncement.read_at"
        class="h-1.5 w-1.5 shrink-0 rounded-full bg-primary-500"
        aria-hidden="true"
      ></span>
      <span class="announcement-ticker-viewport relative block h-5 min-w-0 flex-1 overflow-hidden" aria-live="off">
        <Transition name="announcement-roll">
          <span :key="currentAnnouncement.id" class="announcement-ticker-title block truncate text-sm font-medium leading-5">
            {{ currentAnnouncement.title }}
          </span>
        </Transition>
      </span>
      <span v-if="announcements.length > 1" class="hidden shrink-0 text-[10px] tabular-nums opacity-50 sm:inline" aria-hidden="true">
        {{ currentIndex + 1 }}/{{ announcements.length }}
      </span>
      <Icon name="chevronRight" size="xs" class="shrink-0 opacity-50" aria-hidden="true" />
    </button>
    <button
      v-if="announcements.length > 1"
      type="button"
      class="announcement-ticker-pause mr-1 flex h-7 w-7 shrink-0 items-center justify-center"
      :aria-label="userPaused ? t('announcements.resumeRotation') : t('announcements.pauseRotation')"
      :aria-pressed="userPaused"
      @click="userPaused = !userPaused"
    >
      <svg class="h-3 w-3" fill="currentColor" viewBox="0 0 16 16" aria-hidden="true">
        <path v-if="userPaused" d="M5 3.5a.5.5 0 0 1 .77-.42l7 4.5a.5.5 0 0 1 0 .84l-7 4.5a.5.5 0 0 1-.77-.42z" />
        <path v-else d="M4 3h3v10H4zm5 0h3v10H9z" />
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useDocumentVisibility, useIntervalFn, usePreferredReducedMotion } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { UserAnnouncement } from '@/types'

const props = defineProps<{
  announcements: UserAnnouncement[]
  paused: boolean
}>()
const emit = defineEmits<{ open: [announcement: UserAnnouncement] }>()
const { t } = useI18n()
const currentIndex = ref(0)
const hovered = ref(false)
const focused = ref(false)
const userPaused = ref(false)
const visibility = useDocumentVisibility()
const reducedMotion = usePreferredReducedMotion()
const currentAnnouncement = computed(() => props.announcements[currentIndex.value])

watch(reducedMotion, (preference) => {
  if (preference === 'reduce') userPaused.value = true
}, { immediate: true })

// Keep the displayed item when read state or titles change; restart for a new list.
watch(() => props.announcements.map(item => item.id).join(','), () => {
  currentIndex.value = 0
})

const { pause, resume } = useIntervalFn(() => {
  currentIndex.value = (currentIndex.value + 1) % props.announcements.length
}, 5000, { immediate: false })

watch(
  () => props.announcements.length > 1 && !props.paused && !hovered.value
    && !focused.value && !userPaused.value && visibility.value === 'visible',
  (rotate) => rotate ? resume() : pause(),
  { immediate: true },
)

function handleFocusOut(event: FocusEvent) {
  focused.value = event.relatedTarget instanceof Node
    && (event.currentTarget as HTMLElement).contains(event.relatedTarget)
}
</script>

<style scoped>
.announcement-ticker {
  color: var(--app-control-text, #343437);
  border: 1px solid var(--liquid-edge, rgb(128 128 128 / 18%));
  border-radius: 16px;
  background: var(--liquid-surface, rgb(128 128 128 / 6%));
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 12%);
}

.announcement-ticker-pause {
  opacity: 0.45;
}

.announcement-ticker-pause:hover,
.announcement-ticker-pause:focus-visible {
  background: rgb(128 128 128 / 12%);
  opacity: 1;
}

.announcement-roll-enter-active,
.announcement-roll-leave-active {
  transition: transform 450ms cubic-bezier(0.22, 0.68, 0, 1.05), opacity 350ms ease;
}

.announcement-roll-leave-active {
  position: absolute;
  inset: 0;
}

.announcement-roll-enter-from {
  opacity: 0;
  transform: translateY(100%);
}

.announcement-roll-leave-to {
  opacity: 0;
  transform: translateY(-100%);
}

@media (prefers-reduced-motion: reduce) {
  .announcement-roll-enter-active,
  .announcement-roll-leave-active {
    transition: none;
  }
}
</style>
