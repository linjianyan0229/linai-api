import { afterEach, describe, expect, it, vi } from 'vitest'
import { createLiquidRefraction, liquidDisplacement } from '../liquidRefraction'

afterEach(() => {
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

function sample(map: ReturnType<typeof liquidDisplacement>, x: number, y: number) {
  const offset = (y * map.width + x) * 4
  return Array.from(map.data.slice(offset, offset + 4))
}

describe('outline-fitted glass optics', () => {
  it('transmits the center unchanged and bends the backdrop inward at opposing rims', () => {
    const map = liquidDisplacement(160, 40, 20)
    expect(sample(map, 80, 20)).toEqual([128, 128, 128, 255])
    const left = sample(map, 1, 20)
    const right = sample(map, 158, 20)
    const top = sample(map, 80, 1)
    const bottom = sample(map, 80, 38)
    expect(left[0]).toBeGreaterThan(160)
    expect(right[0]).toBeLessThan(96)
    expect(top[1]).toBeGreaterThan(160)
    expect(bottom[1]).toBeLessThan(96)
    expect(Math.abs(left[1] - 128)).toBeLessThan(5)
    expect(Math.abs(top[0] - 128)).toBeLessThan(2)
    expect(Math.abs(left[0] + right[0] - 255)).toBeLessThan(2)
  })

  it('fits circular corners to the actual aspect ratio and bounds the map cost for large navigation', () => {
    const capsule = liquidDisplacement(160, 40, 20)
    const squared = liquidDisplacement(160, 40, 4)
    // The same corner point is outside a capsule, but on a short-radius bevel.
    expect(sample(capsule, 2, 2)).toEqual([128, 128, 128, 255])
    expect(sample(squared, 2, 2).slice(0, 2)).not.toEqual([128, 128])
    const sidebar = liquidDisplacement(232, 1200, 26)
    expect(sidebar.height).toBe(768)
    expect(sidebar.width / sidebar.height).toBeCloseTo(232 / 1200, 2)
    expect(sidebar.data.length).toBe(sidebar.width * sidebar.height * 4)
    expect(sample(sidebar, Math.floor(sidebar.width / 2), 384)).toEqual([128, 128, 128, 255])
  })

  it('updates actual resized controls, discovers portals, and releases filters on detach and teardown', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('CSS', { supports: () => true })
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => window.setTimeout(() => cb(Date.now()), 16))
    vi.stubGlobal('cancelAnimationFrame', (id: number) => window.clearTimeout(id))
    let resize: ResizeObserverCallback
    const unobserve = vi.fn()
    vi.stubGlobal('ResizeObserver', class {
      constructor(callback: ResizeObserverCallback) { resize = callback }
      observe() {}
      unobserve = unobserve
      disconnect() {}
    })
    let serial = 0
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
      createImageData: (w: number, h: number) => ({ data: new Uint8ClampedArray(w * h * 4) }),
      putImageData: vi.fn()
    } as unknown as CanvasRenderingContext2D)
    vi.spyOn(HTMLCanvasElement.prototype, 'toDataURL').mockImplementation(() => `data:image/png;base64,map${++serial}`)
    document.body.innerHTML = '<svg><defs /></svg><div class="liquid-selection-pill" style="border-radius:20px" />'
    const definitions = document.querySelector('defs')!
    const surface = document.querySelector<HTMLElement>('.liquid-selection-pill')!
    let width = 120
    vi.spyOn(surface, 'getBoundingClientRect').mockImplementation(() => new DOMRect(0, 0, width, 40))
    const cleanup = createLiquidRefraction(document.body, definitions, 'test-optics')
    vi.advanceTimersByTime(20)
    expect(definitions.querySelectorAll('filter')).toHaveLength(1)
    expect(surface.style.getPropertyValue('--liquid-refraction')).toContain('test-optics-surface-1')
    const firstMap = definitions.querySelector('feImage')!.getAttribute('href')
    expect(definitions.querySelector('feImage')!.getAttribute('width')).toBe('120')
    expect(definitions.querySelector('feImage')!.getAttribute('height')).toBe('40')
    width = 180
    resize!([{ target: surface } as unknown as ResizeObserverEntry], {} as ResizeObserver)
    vi.advanceTimersByTime(20)
    expect(definitions.querySelector('feImage')!.getAttribute('href')).not.toBe(firstMap)
    expect(definitions.querySelector('feImage')!.getAttribute('width')).toBe('180')

    const portal = document.createElement('div')
    portal.className = 'liquid-popover'
    vi.spyOn(portal, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 180, 100))
    document.body.appendChild(portal)
    await Promise.resolve()
    vi.advanceTimersByTime(20)
    expect(definitions.querySelectorAll('filter')).toHaveLength(2)
    portal.remove()
    await Promise.resolve()
    vi.advanceTimersByTime(20)
    expect(definitions.querySelectorAll('filter')).toHaveLength(1)
    expect(unobserve).toHaveBeenCalledWith(portal)
    cleanup()
    expect(surface.style.getPropertyValue('--liquid-refraction')).toBe('')
    expect(definitions.querySelectorAll('filter')).toHaveLength(0)
    expect(vi.getTimerCount()).toBe(0)
  })
})
