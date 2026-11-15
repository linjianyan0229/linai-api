<template>
  <div
    v-if="currentAnnouncement"
    class="announcement-ticker flex min-w-0 items-center"
    @mouseenter="hovered = true"
    @mouseleave="handleMouseLeave"
    @wheel="handleWheel"
    @focusin="focused = true"
    @focusout="handleFocusOut"
  >
    <button
      type="button"
      class="flex h-9 min-w-0 flex-1 items-center gap-2 px-3 text-left"
      data-testid="announcement-ticker-open"
      :aria-label="t('announcements.expandAnnouncement', { title: currentAnnouncement.title })"
      :title="announcements.length > 1 ? t('announcements.scrollHint') : undefined"
      aria-haspopup="dialog"
      @click="emit('open', currentAnnouncement)"
    >
      <span v-if="currentAnnouncement.is_pinned" class="shrink-0 rounded-md bg-primary-500/10 px-1.5 py-0.5 text-[10px] font-semibold text-primary-600 dark:text-primary-400">
        {{ t('announcements.pinned') }}
      </span>
      <span
        v-if="!currentAnnouncement.read_at"
        class="h-1.5 w-1.5 shrink-0 rounded-full bg-primary-500"
        aria-hidden="true"
      ></span>
      <span class="announcement-ticker-viewport relative block h-5 min-w-0 flex-1 overflow-hidden" :style="{ '--announcement-roll-direction': direction }" aria-live="off">
        <Transition name="announcement-roll">
          <span :key="currentAnnouncement.id" class="announcement-ticker-title block truncate text-sm font-medium leading-5">
            {{ currentAnnouncement.title }}
          </span>
        </Transition>
      </span>
      <span v-if="announcements.length > 1" class="hidden shrink-0 text-[10px] tabular-nums opacity-50 sm:inline" aria-hidden="true">
        {{ currentIndex + 1 }}/{{ announcements.length }}
      </span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useDocumentVisibility, useIntervalFn, usePreferredReducedMotion } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
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
const visibility = useDocumentVisibility()
const reducedMotion = usePreferredReducedMotion()
const currentAnnouncement = computed(() => props.announcements[currentIndex.value])
const direction = ref(1)
let wheelDelta = 0
let wheelLockedUntil = 0
let lastWheelAt = 0

function resetWheel() {
  wheelDelta = 0
  wheelLockedUntil = 0
  lastWheelAt = 0
}

function move(step: number) {
  direction.value = step
  currentIndex.value = (currentIndex.value + step + props.announcements.length) % props.announcements.length
}

function handleWheel(event: WheelEvent) {
  // Leave zoom gestures, horizontal scrolling and single notices to the browser.
  if (props.paused || props.announcements.length < 2 || event.ctrlKey || event.metaKey
    || !Number.isFinite(event.deltaY) || Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return

  event.preventDefault()
  const now = Date.now()
  if (now < wheelLockedUntil) return

  // Normalize mouse line/page deltas and accumulate small trackpad movements.
  const scale = event.deltaMode === WheelEvent.DOM_DELTA_LINE ? 16
    : event.deltaMode === WheelEvent.DOM_DELTA_PAGE ? 40 : 1
  const delta = event.deltaY * scale
  if (now - lastWheelAt > 200 || Math.sign(delta) !== Math.sign(wheelDelta)) wheelDelta = 0
  lastWheelAt = now
  wheelDelta += delta
  if (Math.abs(wheelDelta) < 40) return

  move(Math.sign(wheelDelta))
  wheelDelta = 0
  wheelLockedUntil = now + 450
}

function handleMouseLeave() {
  hovered.value = false
  resetWheel()
}

// Keep the displayed item when read state or titles change; restart for a new list.
watch(() => props.announcements.map(item => `${item.id}:${!!item.is_pinned}`).join(','), () => {
  currentIndex.value = 0
  direction.value = 1
  resetWheel()
})

const { pause, resume } = useIntervalFn(() => {
  move(1)
}, 5000, { immediate: false })

watch(
  () => props.announcements.length > 1 && !props.paused && !hovered.value
    && !focused.value && reducedMotion.value !== 'reduce' && visibility.value === 'visible',
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
  transform: translateY(calc(100% * var(--announcement-roll-direction, 1)));
}

.announcement-roll-leave-to {
  opacity: 0;
  transform: translateY(calc(-100% * var(--announcement-roll-direction, 1)));
}

@media (prefers-reduced-motion: reduce) {
  .announcement-roll-enter-active,
  .announcement-roll-leave-active {
    transition: none;
  }
}
</style>
