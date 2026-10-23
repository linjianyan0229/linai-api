import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

const controlSelector = [
  'button', '.btn', '.sidebar-link', 'header a', '[role="tab"]'
].join(', ')

/** Delegate light and press feedback to functional controls only. */
export function useLiquidGlass(root: Ref<HTMLElement | null>, enabled: Ref<boolean>) {
  const scrolled = ref(false)
  let surface: HTMLElement | null = null
  let pressed: HTMLElement | null = null
  let frame = 0
  let pointerX = 0
  let pointerY = 0

  function clearLight() {
    if (frame) cancelAnimationFrame(frame)
    frame = 0
    surface?.classList.remove('liquid-interacting')
    surface?.style.removeProperty('--liquid-pointer-x')
    surface?.style.removeProperty('--liquid-pointer-y')
    surface = null
  }

  function releasePress() {
    pressed?.classList.remove('liquid-pressed')
    pressed = null
  }

  function targetControl(event: PointerEvent) {
    if (!enabled.value || !(event.target instanceof Element)) return null
    const control = event.target.closest<HTMLElement>(controlSelector)
    if (!control || !root.value?.contains(control)) return null
    if (control.matches(':disabled, [aria-disabled="true"]')) return null
    // Segmented controls and switches own their continuous glass thumb feedback.
    if (control.closest('[data-liquid-pill]') || control.matches('.liquid-switch')) return null
    // Menu rows use their own selection feedback, without a second glass layer.
    if (control.closest('.date-picker-dropdown, .dropdown, .liquid-popover, .select-dropdown-portal, [data-custom-content]')) return null
    return control
  }

  function updateLight(event: PointerEvent) {
    if (!enabled.value || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    const control = targetControl(event)
    if (control !== surface) clearLight()
    if (!control) return
    surface = control
    surface.classList.add('liquid-interacting')
    pointerX = event.clientX
    pointerY = event.clientY
    if (frame) return
    frame = requestAnimationFrame(() => {
      frame = 0
      if (!surface) return
      const bounds = surface.getBoundingClientRect()
      surface.style.setProperty('--liquid-pointer-x', `${Math.max(0, Math.min(bounds.width, pointerX - bounds.left))}px`)
      surface.style.setProperty('--liquid-pointer-y', `${Math.max(0, Math.min(bounds.height, pointerY - bounds.top))}px`)
    })
  }

  function onPointerMove(event: PointerEvent) {
    if (event.pointerType === 'mouse') updateLight(event)
  }

  function onPointerDown(event: PointerEvent) {
    if (event.button !== 0) return
    releasePress()
    pressed = targetControl(event)
    pressed?.classList.add('liquid-pressed')
    updateLight(event)
  }

  function onScroll() {
    if (enabled.value) scrolled.value = window.scrollY > 8
    clearLight()
    releasePress()
  }

  function onPointerUp(event: PointerEvent) {
    releasePress()
    if (event.pointerType !== 'mouse') clearLight()
  }

  function onWindowBlur() {
    releasePress()
    clearLight()
  }

  onMounted(() => {
    if (!enabled.value) return
    onScroll()
    window.addEventListener('pointerup', onPointerUp)
    window.addEventListener('pointercancel', onPointerUp)
    window.addEventListener('blur', onWindowBlur)
    window.addEventListener('scroll', onScroll, { passive: true, capture: true })
  })

  onBeforeUnmount(() => {
    clearLight()
    releasePress()
    window.removeEventListener('pointerup', onPointerUp)
    window.removeEventListener('pointercancel', onPointerUp)
    window.removeEventListener('blur', onWindowBlur)
    window.removeEventListener('scroll', onScroll, true)
  })

  return { scrolled, onPointerMove, onPointerDown, clearLight }
}
