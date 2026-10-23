<template>
  <span v-if="version" class="inline-flex rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-800 dark:text-dark-400">
    {{ version.startsWith('v') ? version : `v${version}` }}
  </span>
</template>

<script setup lang="ts">
defineProps<{ version?: string }>()
</script>
