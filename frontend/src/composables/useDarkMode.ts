import { onBeforeUnmount, onMounted, readonly, ref } from 'vue'

/** Keep canvas colors in sync with the class used by the CSS theme. */
export function useDarkMode() {
  const isDarkMode = ref(document.documentElement.classList.contains('dark'))
  const observer = new MutationObserver(() => {
    isDarkMode.value = document.documentElement.classList.contains('dark')
  })

  onMounted(() => {
    isDarkMode.value = document.documentElement.classList.contains('dark')
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  })
  onBeforeUnmount(() => observer.disconnect())

  return readonly(isDarkMode)
}
