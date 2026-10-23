import type { ObjectDirective } from 'vue'

type Bounds = { left: number; right: number; top: number; bottom: number }
export type LiquidPillOptions = {
  axis?: 'horizontal' | 'vertical'
  optionSelector?: string
  selectedSelector?: string
  draggable?: boolean
  persistenceKey?: string
}
type PillController = { update: (options?: LiquidPillOptions) => void; destroy: () => void }
type SavedPill = { bounds: Bounds; velocity: Bounds; width: number; time: number }

const controllers = new WeakMap<HTMLElement, PillController>()
const savedPills = new Map<string, SavedPill>()
const defaultOptionSelector = '[role="tab"], [role="radio"], button[aria-pressed], [data-liquid-option]'
const defaultSelectedSelector = '[aria-selected="true"], [aria-pressed="true"], [aria-current="page"], [aria-checked="true"]'

/** One persistent glass shape follows selection; existing buttons keep their own actions and semantics. */
export function createLiquidPill(group: HTMLElement, settings: LiquidPillOptions = {}): PillController {
  const pill = document.createElement('span')
  pill.className = 'liquid-selection-pill'
  pill.setAttribute('aria-hidden', 'true')
  group.prepend(pill)
  group.setAttribute('data-liquid-pill', '')

  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)')
  const velocity: Bounds = { left: 0, right: 0, top: 0, bottom: 0 }
  let current: Bounds | null = null
  let destination: Bounds | null = null
  let selected: HTMLElement | null = null
  let options: HTMLElement[] = []
  let frame = 0
  let updateFrame = 0
  let lastTime = 0
  let alive = true
  let pointer: { id: number; x: number; y: number; touch: boolean; dragging: boolean } | null = null
  let suppressClickUntil = 0
  let config = settings

  function remember() {
    if (!config.persistenceKey || !current) return
    savedPills.set(config.persistenceKey, {
      bounds: { ...current }, velocity: { ...velocity },
      width: group.getBoundingClientRect().width, time: performance.now()
    })
  }

  function bounds(element: HTMLElement): Bounds {
    const container = group.getBoundingClientRect()
    const rect = element.getBoundingClientRect()
    const left = rect.left - container.left + group.scrollLeft - group.clientLeft
    const top = rect.top - container.top + group.scrollTop - group.clientTop
    return { left, right: left + rect.width, top, bottom: top + rect.height }
  }

  function paint() {
    if (!current) return
    pill.style.transform = `translate3d(${current.left}px, ${current.top}px, 0)`
    pill.style.width = `${Math.max(0, current.right - current.left)}px`
    pill.style.height = `${Math.max(0, current.bottom - current.top)}px`
  }

  function stop() {
    cancelAnimationFrame(frame)
    frame = 0
    lastTime = 0
    for (const edge of Object.keys(velocity) as Array<keyof Bounds>) velocity[edge] = 0
  }

  function animate(time: number) {
    frame = 0
    if (!alive || !current || !destination) return
    const dt = Math.min(0.032, Math.max(0.001, lastTime ? (time - lastTime) / 1000 : 1 / 60))
    lastTime = time
    const vertical = config.axis === 'vertical'
    const forward = vertical
      ? destination.top + destination.bottom >= current.top + current.bottom
      : destination.left + destination.right >= current.left + current.right
    let settled = true
    for (const edge of Object.keys(current) as Array<keyof Bounds>) {
      // The leading rim moves faster than the trailing rim, stretching one continuous capsule.
      const leading = edge === (vertical ? (forward ? 'bottom' : 'top') : (forward ? 'right' : 'left'))
      const stiffness = pointer?.dragging ? 850 : leading ? 620 : 430
      const damping = pointer?.dragging ? 48 : leading ? 37 : 31
      velocity[edge] += ((destination[edge] - current[edge]) * stiffness - velocity[edge] * damping) * dt
      current[edge] += velocity[edge] * dt
      if (Math.abs(destination[edge] - current[edge]) > 0.08 || Math.abs(velocity[edge]) > 0.3) settled = false
    }
    if (settled) {
      current = { ...destination }
      stop()
      group.removeAttribute('data-liquid-moving')
    } else frame = requestAnimationFrame(animate)
    paint()
  }

  function move(next: Bounds, snap = false) {
    destination = next
    if (!current && config.persistenceKey && !reducedMotion.matches) {
      const saved = savedPills.get(config.persistenceKey)
      savedPills.delete(config.persistenceKey)
      // Route views remount their layout. Carry the live shape and its momentum
      // across that handoff, but never across a different layout or a later visit.
      if (saved && performance.now() - saved.time < 800 && Math.abs(saved.width - group.getBoundingClientRect().width) < 2) {
        current = { ...saved.bounds }
        Object.assign(velocity, saved.velocity)
        paint()
        snap = false
      }
    }
    if (!current || snap || reducedMotion.matches) {
      stop()
      current = { ...next }
      group.removeAttribute('data-liquid-moving')
      paint()
    } else if (!frame) {
      group.setAttribute('data-liquid-moving', '')
      frame = requestAnimationFrame(animate)
    }
  }

  function measure(snap = false) {
    if (!alive || pointer?.dragging) return
    // Long tab bars keep native touch scrolling on narrow screens.
    let scrollable = false
    for (let parent: HTMLElement | null = group; parent; parent = parent.parentElement) {
      if (parent.scrollWidth > parent.clientWidth + 2 && /auto|scroll/.test(getComputedStyle(parent).overflowX)) {
        scrollable = true
        break
      }
    }
    group.toggleAttribute('data-liquid-scrollable', scrollable)
    const visible = options.map(option => option.getBoundingClientRect()).filter(rect => rect.width > 0)
    group.toggleAttribute('data-liquid-wrapped', config.axis !== 'vertical' && visible.some(rect => Math.abs(rect.top - visible[0].top) > 2))
    const next = options.find(option => option.matches(config.selectedSelector ?? defaultSelectedSelector)) ?? null
    if (selected !== next) selected?.classList.remove('liquid-pill-current')
    selected = next
    selected?.classList.add('liquid-pill-current')
    const rect = selected && bounds(selected)
    const shown = !!rect && rect.right > rect.left && rect.bottom > rect.top
    pill.hidden = !shown
    if (rect && shown) move(rect, snap)
    else {
      stop()
      current = null
    }
  }

  // Selection can change a label's width; keep its spring alive while remeasuring.
  const resize = new ResizeObserver(() => measure(!!current && !frame))
  resize.observe(group)

  function update(settings?: LiquidPillOptions) {
    if (!alive) return
    if (settings) {
      if (settings.persistenceKey !== config.persistenceKey) { remember(); current = null; stop() }
      config = settings
    }
    group.setAttribute('data-liquid-axis', config.axis ?? 'horizontal')
    const nextOptions = Array.from(group.querySelectorAll<HTMLElement>(config.optionSelector ?? defaultOptionSelector))
      .filter(option => option.closest('[data-liquid-pill]') === group)
    for (const option of options) {
      if (!nextOptions.includes(option)) {
        resize.unobserve?.(option)
        option.classList.remove('liquid-pill-current')
      }
    }
    for (const option of nextOptions) if (!options.includes(option)) resize.observe(option)
    options = nextOptions
    cancelAnimationFrame(updateFrame)
    updateFrame = requestAnimationFrame(() => {
      updateFrame = 0
      measure()
    })
  }

  function enabledOptions() {
    return options.filter(option => !option.matches(':disabled, [aria-disabled="true"]'))
  }

  function nearest(x: number, y: number) {
    let closest: HTMLElement | null = null
    let distance = Infinity
    for (const option of enabledOptions()) {
      const rect = option.getBoundingClientRect()
      const next = Math.hypot(x - (rect.left + rect.width / 2), y - (rect.top + rect.height / 2))
      if (next < distance) { distance = next; closest = option }
    }
    return closest
  }

  function down(event: PointerEvent) {
    if (event.button !== 0 || !event.isPrimary || !(event.target instanceof Element)) return
    suppressClickUntil = 0
    const option = event.target.closest<HTMLElement>(config.optionSelector ?? defaultOptionSelector)
    if (!option || !enabledOptions().includes(option)) return
    pointer = { id: event.pointerId, x: event.clientX, y: event.clientY, touch: event.pointerType === 'touch', dragging: false }
    group.setAttribute('data-liquid-pressing', '')
  }

  function drag(event: PointerEvent) {
    if (!pointer || event.pointerId !== pointer.id) return
    const dx = event.clientX - pointer.x
    const dy = event.clientY - pointer.y
    if (!pointer.dragging) {
      if (Math.hypot(dx, dy) < 6) return
      if (config.draggable === false) { release(); return }
      if (pointer.touch && group.hasAttribute('data-liquid-scrollable')) { release(); return }
      // A vertical touch gesture continues to scroll the page.
      if (pointer.touch && Math.abs(dy) > Math.abs(dx)) { release(); return }
      pointer.dragging = true
      group.setPointerCapture?.(event.pointerId)
      group.setAttribute('data-liquid-dragging', '')
    }
    event.preventDefault()
    const option = nearest(event.clientX, event.clientY)
    if (!option) return
    const rect = bounds(option)
    const container = group.getBoundingClientRect()
    const width = rect.right - rect.left
    const visibleOptions = enabledOptions().map(bounds)
    const min = Math.min(...visibleOptions.map(item => item.left))
    const max = Math.max(...visibleOptions.map(item => item.right)) - width
    const left = Math.max(min, Math.min(max, event.clientX - container.left + group.scrollLeft - group.clientLeft - width / 2))
    move({ left, right: left + width, top: rect.top - 1, bottom: rect.bottom + 1 })
    pill.style.setProperty('--liquid-drag-light', `${Math.max(10, Math.min(90, (event.clientX - container.left - left) / width * 100))}%`)
  }

  function finishPointer() {
    const id = pointer?.id
    pointer = null
    group.removeAttribute('data-liquid-pressing')
    group.removeAttribute('data-liquid-dragging')
    if (id !== undefined && group.hasPointerCapture?.(id)) group.releasePointerCapture(id)
  }

  function release() { finishPointer(); measure() }

  function up(event: PointerEvent) {
    if (!pointer || pointer.id !== event.pointerId) return
    const option = pointer.dragging ? nearest(event.clientX, event.clientY) : null
    if (pointer.dragging) suppressClickUntil = performance.now() + 350
    // Reuse the view's existing action exactly once; the native trailing click is suppressed below.
    if (option) {
      finishPointer()
      option.click()
      update()
    } else release()
  }

  function click(event: MouseEvent) {
    if (event.isTrusted && event.detail > 0 && performance.now() < suppressClickUntil) {
      suppressClickUntil = 0
      event.preventDefault()
      event.stopImmediatePropagation()
      return
    }
    // Start a navigation response immediately, even while the next route loads.
    if (config.draggable === false && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey && event.target instanceof Element) {
      const option = event.target.closest<HTMLElement>('a[href]')
      if (option && options.includes(option)) move(bounds(option))
    }
  }

  function light(event: PointerEvent) {
    if (!current || reducedMotion.matches) return
    const rect = group.getBoundingClientRect()
    const x = (event.clientX - rect.left + group.scrollLeft - current.left) / (current.right - current.left)
    const y = (event.clientY - rect.top + group.scrollTop - current.top) / (current.bottom - current.top)
    pill.style.setProperty('--liquid-glint-x', `${Math.max(0, Math.min(100, x * 100))}%`)
    pill.style.setProperty('--liquid-glint-y', `${Math.max(0, Math.min(100, y * 100))}%`)
  }

  function cancel(event: PointerEvent) { if (pointer?.id === event.pointerId) release() }
  function motionChanged() { if (reducedMotion.matches) { release(); measure(true) } }
  group.addEventListener('pointerdown', down)
  group.addEventListener('pointermove', light, { passive: true })
  group.addEventListener('click', click, true)
  window.addEventListener('pointermove', drag, { passive: false })
  window.addEventListener('pointerup', up)
  window.addEventListener('pointercancel', cancel)
  window.addEventListener('blur', release)
  reducedMotion.addEventListener('change', motionChanged)
  document.fonts?.ready.then(() => { if (alive) measure(!frame) })
  update()

  return {
    update,
    destroy() {
      remember()
      alive = false
      release()
      stop()
      cancelAnimationFrame(updateFrame)
      resize.disconnect()
      group.removeEventListener('pointerdown', down)
      group.removeEventListener('pointermove', light)
      group.removeEventListener('click', click, true)
      window.removeEventListener('pointermove', drag)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', cancel)
      window.removeEventListener('blur', release)
      reducedMotion.removeEventListener('change', motionChanged)
      for (const option of options) option.classList.remove('liquid-pill-current')
      group.removeAttribute('data-liquid-pill')
      group.removeAttribute('data-liquid-axis')
      group.removeAttribute('data-liquid-moving')
      group.removeAttribute('data-liquid-scrollable')
      group.removeAttribute('data-liquid-wrapped')
      pill.remove()
    }
  }
}

export const vLiquidPill: ObjectDirective<HTMLElement, LiquidPillOptions | undefined> = {
  mounted(group, binding) { controllers.set(group, createLiquidPill(group, binding.value)) },
  updated(group, binding) { controllers.get(group)?.update(binding.value) },
  beforeUnmount(group) { controllers.get(group)?.destroy(); controllers.delete(group) }
}
