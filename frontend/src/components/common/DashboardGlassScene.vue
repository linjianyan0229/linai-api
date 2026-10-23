<template>
  <div class="liquid-dashboard space-y-6">
    <section class="dashboard-glass-hero">
      <div class="min-w-0">
        <div class="dashboard-date-pill">
          <Icon name="calendar" size="sm" />
          <span>{{ formattedDate }}</span>
        </div>
        <h2 class="dashboard-glass-title">{{ title }}</h2>
        <p class="dashboard-glass-description">{{ description }}</p>
      </div>
    </section>
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

defineProps<{ title: string; description: string }>()

const { locale } = useI18n()
const today = new Date()
const formattedDate = computed(() => new Intl.DateTimeFormat(
  locale?.value === 'zh' ? 'zh-CN' : 'en-US',
  { month: 'long', day: 'numeric', weekday: 'long' }
).format(today))

</script>
