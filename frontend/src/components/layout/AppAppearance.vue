<template>
  <LiquidGlassLens :id="lensId" />
  <slot />
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import LiquidGlassLens from '@/components/common/LiquidGlassLens.vue'
import { useLiquidGlass } from '@/composables/useLiquidGlass'
import '@/styles/liquid-glass.css'
import '@/styles/liquid-controls.css'

// One owner for the page and its teleported menus/dialogs, across route changes.
const root = ref<HTMLElement | null>(null)
const enabled = ref(true)
const lensId = `liquid-lens-${useId()}`
const { scrolled, onPointerMove, onPointerDown, clearLight } = useLiquidGlass(root, enabled)

watch(scrolled, value => document.body.classList.toggle('liquid-scrolled', value))

onMounted(() => {
  root.value = document.body
  document.body.classList.add('liquid-app')
  document.body.style.setProperty('--liquid-refraction', `url(#${lensId})`)
  document.addEventListener('pointermove', onPointerMove, { passive: true })
  document.addEventListener('pointerdown', onPointerDown, { passive: true })
  document.documentElement.addEventListener('pointerleave', clearLight)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointermove', onPointerMove)
  document.removeEventListener('pointerdown', onPointerDown)
  document.documentElement.removeEventListener('pointerleave', clearLight)
  document.body.classList.remove('liquid-app', 'liquid-scrolled')
  document.body.style.removeProperty('--liquid-refraction')
})
</script>
