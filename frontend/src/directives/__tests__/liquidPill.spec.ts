import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createLiquidPill } from '../liquidPill'

let group: HTMLElement
let controller: ReturnType<typeof createLiquidPill> | undefined
let reduced = false
let resize: () => void
let secondLeft = 140
let thirdLeft = 260
let motionChanged: () => void

function pointer(target: EventTarget, type: string, x: number, y = 30, pointerType = 'mouse') {
  const event = new MouseEvent(type, { bubbles: true, cancelable: true, button: 0, clientX: x, clientY: y })
  Object.defineProperties(event, { pointerId: { value: 1 }, isPrimary: { value: true }, pointerType: { value: pointerType } })
  target.dispatchEvent(event)
}

function active(index: number) {
  const buttons = group.querySelectorAll('button')
  buttons.forEach((button, i) => button.setAttribute('aria-pressed', String(index === i)))
  controller!.update()
}

function geometry() {
  const pill = group.querySelector<HTMLElement>('.liquid-selection-pill')!
  const x = Number(pill.style.transform.match(/translate3d\(([-.\d]+)px/)?.[1])
  return { x, width: parseFloat(pill.style.width), hidden: pill.hidden }
}

beforeEach(() => {
  vi.useFakeTimers()
  reduced = false
  secondLeft = 140
  thirdLeft = 260
  vi.spyOn(window, 'matchMedia').mockImplementation(() => ({
    get matches() { return reduced },
    addEventListener: (_type: string, listener: () => void) => { motionChanged = listener },
    removeEventListener: vi.fn()
  }) as unknown as MediaQueryList)
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => window.setTimeout(() => cb(Date.now()), 16))
  vi.stubGlobal('cancelAnimationFrame', (id: number) => window.clearTimeout(id))
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: () => void) { resize = callback }
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  group = document.createElement('div')
  group.innerHTML = '<button aria-pressed="true">First</button><button aria-pressed="false">Second</button><button aria-pressed="false">Third</button>'
  document.body.appendChild(group)
  vi.spyOn(group, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 400, 60))
  Array.from(group.querySelectorAll('button')).forEach((button, i) => {
    vi.spyOn(button, 'getBoundingClientRect').mockImplementation(() => new DOMRect(i === 0 ? 4 : i === 1 ? secondLeft : thirdLeft, 4, i === 1 ? 112 : 96, 36))
    button.addEventListener('click', () => active(i))
  })
  controller = createLiquidPill(group)
  vi.advanceTimersByTime(20)
})

afterEach(() => {
  controller?.destroy()
  controller = undefined
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('continuous Liquid Glass selection capsule', () => {
  it('moves one persistent shape, stretches during travel, and settles on unequal option widths', () => {
    const pill = group.querySelector('.liquid-selection-pill')
    active(1)
    vi.advanceTimersByTime(96)
    const moving = geometry()
    expect(moving.x).toBeGreaterThan(4)
    expect(moving.x).toBeLessThan(140)
    expect(moving.width).toBeGreaterThan(112)
    vi.advanceTimersByTime(1600)
    expect(geometry()).toEqual({ x: 140, width: 112, hidden: false })
    expect(group.querySelector('.liquid-selection-pill')).toBe(pill)
    expect(group.hasAttribute('data-liquid-moving')).toBe(false)
  })

  it('retargets an unfinished spring and recomputes bounds on resize', () => {
    active(2)
    vi.advanceTimersByTime(80)
    active(0)
    vi.advanceTimersByTime(1800)
    expect(geometry().x).toBe(4)
    active(1)
    vi.advanceTimersByTime(1800)
    secondLeft = 172
    resize()
    expect(geometry().x).toBe(172)
  })

  it('drags to another enabled option and dispatches its existing action once', () => {
    const first = group.querySelectorAll('button')[0]
    const last = group.querySelectorAll('button')[2]
    const action = vi.fn()
    last.addEventListener('click', action)
    pointer(first, 'pointerdown', 50)
    pointer(window, 'pointermove', 310)
    expect(group.hasAttribute('data-liquid-dragging')).toBe(true)
    vi.advanceTimersByTime(160)
    const beforeRelease = geometry().x
    pointer(window, 'pointerup', 310)
    vi.advanceTimersByTime(16)
    expect(geometry().x).toBeGreaterThanOrEqual(beforeRelease)
    vi.advanceTimersByTime(1800)
    expect(action).toHaveBeenCalledTimes(1)
    expect(last.getAttribute('aria-pressed')).toBe('true')
    expect(geometry().x).toBe(260)
    expect(group.hasAttribute('data-liquid-dragging')).toBe(false)
  })

  it('skips disabled options and cancels a drag without changing selection', () => {
    const first = group.querySelectorAll('button')[0]
    group.querySelectorAll('button')[1].disabled = true
    pointer(first, 'pointerdown', 50)
    pointer(window, 'pointermove', 195)
    pointer(window, 'pointercancel', 195)
    vi.advanceTimersByTime(1600)
    expect(first.getAttribute('aria-pressed')).toBe('true')
    expect(geometry().x).toBe(4)
  })

  it('snaps under reduced motion and cleans up an active animation and all pointer feedback', () => {
    active(2)
    vi.advanceTimersByTime(60)
    reduced = true
    motionChanged()
    expect(geometry().x).toBe(260)
    expect(group.hasAttribute('data-liquid-moving')).toBe(false)
    controller!.destroy()
    controller = undefined
    pointer(group.querySelector('button')!, 'pointerdown', 50)
    expect(group.querySelector('.liquid-selection-pill')).toBeNull()
    expect(group.hasAttribute('data-liquid-pressing')).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('preserves native touch scrolling for a long tab bar in a narrow container', () => {
    const scroller = document.createElement('div')
    scroller.style.overflowX = 'auto'
    Object.defineProperties(scroller, { clientWidth: { value: 240 }, scrollWidth: { value: 400 } })
    document.body.appendChild(scroller)
    scroller.appendChild(group)
    resize()
    expect(group.hasAttribute('data-liquid-scrollable')).toBe(true)
    pointer(group.querySelector('button')!, 'pointerdown', 50, 30, 'touch')
    pointer(window, 'pointermove', 190, 30, 'touch')
    pointer(window, 'pointerup', 190, 30, 'touch')
    expect(group.hasAttribute('data-liquid-dragging')).toBe(false)
    expect(group.querySelector('button')!.getAttribute('aria-pressed')).toBe('true')
  })

  it('carries the vertical lens and its momentum across a route layout remount without intercepting sidebar scroll', () => {
    controller!.destroy()
    const settings = { axis: 'vertical' as const, draggable: false, persistenceKey: 'sidebar-route-test', optionSelector: 'a' }
    let width = 232
    function navigation() {
      group.innerHTML = '<a href="/first" aria-current="page">First</a><a href="/second">Second</a><a href="/third">Third</a>'
      vi.spyOn(group, 'getBoundingClientRect').mockImplementation(() => new DOMRect(0, 0, width, 320))
      Array.from(group.querySelectorAll('a')).forEach((link, i) => {
        vi.spyOn(link, 'getBoundingClientRect').mockImplementation(() => new DOMRect(10, 12 + i * 44, width - 22, 40))
      })
    }
    function position() {
      const pill = group.querySelector<HTMLElement>('.liquid-selection-pill')!
      const y = Number(pill.style.transform.match(/, ([-.\d]+)px/)?.[1])
      return { y, height: parseFloat(pill.style.height), width: parseFloat(pill.style.width) }
    }
    navigation()
    controller = createLiquidPill(group, settings)
    vi.advanceTimersByTime(20)
    const links = group.querySelectorAll('a')
    links[0].removeAttribute('aria-current')
    links[2].setAttribute('aria-current', 'page')
    controller.update()
    vi.advanceTimersByTime(96)
    const traveling = position()
    expect(traveling.y).toBeGreaterThan(12)
    expect(traveling.y).toBeLessThan(100)
    expect(traveling.height).toBeGreaterThan(40)

    controller.destroy()
    group.remove()
    group = document.createElement('nav')
    document.body.appendChild(group)
    navigation()
    group.querySelectorAll('a')[0].removeAttribute('aria-current')
    group.querySelectorAll('a')[2].setAttribute('aria-current', 'page')
    controller = createLiquidPill(group, settings)
    vi.advanceTimersByTime(16)
    expect(position()).toEqual(traveling)
    vi.advanceTimersByTime(32)
    expect(position().y).toBeGreaterThan(traveling.y)

    const action = vi.fn()
    group.querySelector('a')!.addEventListener('click', action)
    pointer(group.querySelector('a')!, 'pointerdown', 30, 30, 'touch')
    pointer(window, 'pointermove', 30, 190, 'touch')
    pointer(window, 'pointerup', 30, 190, 'touch')
    expect(action).not.toHaveBeenCalled()
    expect(group.hasAttribute('data-liquid-dragging')).toBe(false)
    vi.advanceTimersByTime(1800)
    expect(position()).toEqual({ y: 100, height: 40, width: 210 })

    // A collapsed or resized navigation must not reuse an incompatible old shape.
    controller.destroy()
    width = 64
    controller = createLiquidPill(group, settings)
    vi.advanceTimersByTime(20)
    expect(position()).toEqual({ y: 100, height: 40, width: 42 })
  })
})
