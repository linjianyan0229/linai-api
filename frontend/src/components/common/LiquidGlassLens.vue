<template>
  <svg class="liquid-glass-filter" width="0" height="0" aria-hidden="true" focusable="false">
    <defs ref="definitions">
      <filter :id="id" x="0" y="0" width="100%" height="100%" color-interpolation-filters="sRGB">
        <feGaussianBlur stdDeviation="0" />
      </filter>
    </defs>
  </svg>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { createLiquidRefraction } from '@/utils/liquidRefraction'

const props = defineProps<{ id: string }>()
const definitions = ref<SVGDefsElement | null>(null)
let cleanup = () => {}
onMounted(() => {
  if (definitions.value) cleanup = createLiquidRefraction(document.body, definitions.value, props.id)
})
onBeforeUnmount(() => cleanup())

</script>
