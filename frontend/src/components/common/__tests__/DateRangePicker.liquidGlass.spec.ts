import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import DateRangePicker from '../DateRangePicker.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key, locale: ref('en') })
}))

let wrapper: VueWrapper | undefined
const originalWidth = window.innerWidth
const originalHeight = window.innerHeight

afterEach(() => {
  wrapper?.unmount()
  vi.restoreAllMocks()
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: originalWidth })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: originalHeight })
})

async function openAt(top: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 375 })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: 740 })
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(new DOMRect(280, top, 80, 40))
  wrapper = mount(DateRangePicker, {
    props: { startDate: '2026-10-21', endDate: '2026-10-22', appearance: 'liquid-glass' }
  })
  await wrapper.get('.date-picker-trigger').trigger('click')
  return wrapper.get<HTMLElement>('.date-picker-dropdown').element
}

describe('Liquid Glass date picker placement', () => {
  it('keeps the menu inside a narrow viewport, including after resizing', async () => {
    const menu = await openAt(80)
    expect(parseFloat(menu.style.left)).toBeGreaterThanOrEqual(16)
    expect(parseFloat(menu.style.left) + parseFloat(menu.style.width)).toBeLessThanOrEqual(359)

    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 320 })
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(parseFloat(menu.style.left) + parseFloat(menu.style.width)).toBeLessThanOrEqual(304)
  })

  it('opens above a low trigger and dismisses with Escape without applying changes', async () => {
    const menu = await openAt(650)
    expect(parseFloat(menu.style.bottom)).toBeGreaterThan(740 - 650)
    expect(parseFloat(menu.style.maxHeight)).toBeGreaterThan(0)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper!.get('.date-picker-trigger').attributes('aria-expanded')).toBe('false')
    expect(wrapper!.emitted('change')).toBeUndefined()
  })
})
