<template>
  <button
    ref="control"
    type="button"
    class="liquid-switch"
    :class="{ 'liquid-switch-small': size === 'sm' }"
    :style="{ '--switch-progress': progress }"
    :data-pressing="pressing || undefined"
    :data-dragging="dragging || undefined"
    :disabled="disabled"
    role="switch"
    :aria-checked="modelValue"
    @click="toggle"
    @pointerdown="pointerDown"
    @pointermove="pointerMove"
    @pointerup="pointerUp"
    @pointercancel="cancel"
    @lostpointercapture="cancel"
  >
    <span class="liquid-switch-thumb" aria-hidden="true" />
  </button>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: boolean
  disabled?: boolean
  size?: 'sm' | 'md'
}>(), { disabled: false, size: 'md' })

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
}>()

const control = ref<HTMLButtonElement | null>(null)
const pressing = ref(false)
const dragging = ref(false)
const dragProgress = ref<number | null>(null)
const progress = computed(() => dragProgress.value ?? (props.modelValue ? 1 : 0))
let pointer: { id: number; x: number; y: number; value: number } | null = null
let suppressClickUntil = 0

function toggle(event: MouseEvent) {
  if (props.disabled) return
  if (event.detail > 0 && performance.now() < suppressClickUntil) { suppressClickUntil = 0; return }
  emit('update:modelValue', !props.modelValue)
}

function pointerDown(event: PointerEvent) {
  if (props.disabled || event.button !== 0 || !event.isPrimary) return
  suppressClickUntil = 0
  pointer = { id: event.pointerId, x: event.clientX, y: event.clientY, value: props.modelValue ? 1 : 0 }
  pressing.value = true
  control.value?.setPointerCapture?.(event.pointerId)
}

function pointerMove(event: PointerEvent) {
  if (!pointer || event.pointerId !== pointer.id || props.disabled) return
  const dx = event.clientX - pointer.x
  const dy = event.clientY - pointer.y
  if (!dragging.value) {
    if (Math.hypot(dx, dy) < 4) return
    if (event.pointerType === 'touch' && Math.abs(dy) > Math.abs(dx)) { cancel(); return }
    dragging.value = true
  }
  const rtl = control.value && getComputedStyle(control.value).direction === 'rtl'
  const travel = props.size === 'sm' ? 16 : 20
  dragProgress.value = Math.max(0, Math.min(1, pointer.value + (rtl ? -dx : dx) / travel))
}

function pointerUp(event: PointerEvent) {
  if (!pointer || event.pointerId !== pointer.id) return
  const value = progress.value >= 0.5
  if (dragging.value) {
    suppressClickUntil = performance.now() + 350
    if (!props.disabled && value !== props.modelValue) emit('update:modelValue', value)
  }
  cancel()
}

function cancel() {
  const id = pointer?.id
  pointer = null
  pressing.value = false
  dragging.value = false
  dragProgress.value = null
  if (id !== undefined && control.value?.hasPointerCapture?.(id)) control.value.releasePointerCapture(id)
}

watch(() => props.disabled, value => { if (value) cancel() })
onMounted(() => window.addEventListener('blur', cancel))
onBeforeUnmount(() => { cancel(); window.removeEventListener('blur', cancel) })
</script>
