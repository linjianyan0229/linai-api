import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import AnnouncementTicker from '../AnnouncementTicker.vue'
import type { UserAnnouncement } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const notice = (id: number): UserAnnouncement => ({
  id, title: `Notice ${id}`, content: 'Full content', notify_mode: 'silent',
  created_at: '2026-10-25', updated_at: '2026-10-25',
})

const mountTicker = (announcements = [notice(1), notice(2), notice(3)]) => mount(AnnouncementTicker, {
  props: { announcements, paused: false },
  global: { stubs: { Transition: true } },
})

function wheel(wrapper: ReturnType<typeof mountTicker>, options: WheelEventInit = { deltaY: 100 }) {
  const event = new WheelEvent('wheel', { bubbles: true, cancelable: true, ...options })
  wrapper.get('.announcement-ticker').element.dispatchEvent(event)
  return event
}

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.useFakeTimers()
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
  vi.spyOn(window, 'matchMedia').mockImplementation(query => ({
    matches: false, media: query, onchange: null,
    addListener: vi.fn(), removeListener: vi.fn(),
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn(),
  }))
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('announcement ticker', () => {
  it('browses both ways on hover, wraps and opens the displayed history item', async () => {
    const wrapper = mountTicker()
    await wrapper.trigger('mouseenter')
    expect(wheel(wrapper).defaultPrevented).toBe(true)
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 2')
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 2')
    wheel(wrapper, { deltaY: -100 })
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 1')
    expect(wrapper.get('.announcement-ticker-viewport').attributes('style')).toContain('--announcement-roll-direction: -1')
    await vi.advanceTimersByTimeAsync(450)
    wheel(wrapper, { deltaY: -100 })
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 3')
    expect(wrapper.emitted('open')).toBeUndefined()
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('open')?.[0]).toEqual([notice(3)])
    await wrapper.trigger('mouseleave')
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 1')
  })

  it('accumulates trackpad movement and limits rapid events during an animation', async () => {
    const wrapper = mountTicker()
    await wrapper.trigger('mouseenter')
    for (let i = 0; i < 7; i++) wheel(wrapper, { deltaY: 5 })
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 1')
    for (let i = 0; i < 40; i++) wheel(wrapper, { deltaY: 5 })
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 2')
    await vi.advanceTimersByTimeAsync(450)
    wheel(wrapper, { deltaY: 40 })
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 3')
  })

  it('normalizes line and page scrolling and discards stale partial gestures', async () => {
    const wrapper = mountTicker()
    await wrapper.trigger('mouseenter')
    wheel(wrapper, { deltaY: 20 })
    await vi.advanceTimersByTimeAsync(250)
    wheel(wrapper, { deltaY: 20 })
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 1')
    wheel(wrapper, { deltaY: -3, deltaMode: WheelEvent.DOM_DELTA_LINE })
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 3')
    await vi.advanceTimersByTimeAsync(450)
    wheel(wrapper, { deltaY: 1, deltaMode: WheelEvent.DOM_DELTA_PAGE })
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 1')
  })

  it('preserves zoom, horizontal scrolling and page scrolling without multiple notices', async () => {
    const wrapper = mountTicker()
    for (const options of [
      { deltaY: 100, ctrlKey: true }, { deltaY: 100, metaKey: true },
      { deltaY: 10, deltaX: 100 }, { deltaY: 0 },
    ]) expect(wheel(wrapper, options).defaultPrevented).toBe(false)
    await wrapper.setProps({ paused: true })
    expect(wheel(wrapper).defaultPrevented).toBe(false)
    await wrapper.setProps({ paused: false, announcements: [notice(1)] })
    expect(wheel(wrapper).defaultPrevented).toBe(false)
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 1')
  })

  it('allows manual wheel navigation when reduced motion disables autoplay', async () => {
    vi.mocked(window.matchMedia).mockImplementation(query => ({
      matches: query === '(prefers-reduced-motion: reduce)', media: query, onchange: null,
      addListener: vi.fn(), removeListener: vi.fn(),
      addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn(),
    }))
    const wrapper = mountTicker()
    await wrapper.trigger('mouseenter')
    wheel(wrapper)
    await nextTick()
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 2')
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 2')
  })

  it('immediately returns to a newly pinned first notice even when IDs are unchanged', async () => {
    const wrapper = mountTicker()
    await nextTick()
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 2')
    await wrapper.setProps({ announcements: [{ ...notice(1), is_pinned: true }, notice(2), notice(3)] })
    expect(wrapper.get('.announcement-ticker-title').text()).toBe('Notice 1')
    expect(wrapper.text()).toContain('announcements.pinned')
  })
  it('rotates vertically through the notices and wraps to the first', async () => {
    const wrapper = mountTicker()
    await nextTick()
    const title = () => wrapper.get('.announcement-ticker-title').text()
    expect(title()).toBe('Notice 1')
    await vi.advanceTimersByTimeAsync(5000)
    expect(title()).toBe('Notice 2')
    await vi.advanceTimersByTimeAsync(5000)
    expect(title()).toBe('Notice 3')
    await vi.advanceTimersByTimeAsync(5000)
    expect(title()).toBe('Notice 1')
  })

  it('pauses on hover, keyboard focus and while a dialog is open', async () => {
    const wrapper = mountTicker()
    await wrapper.trigger('mouseenter')
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.text()).toContain('Notice 1')
    await wrapper.trigger('mouseleave')
    await wrapper.trigger('focusin')
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.text()).toContain('Notice 1')
    await wrapper.trigger('focusout', { relatedTarget: null })
    await wrapper.setProps({ paused: true })
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.text()).toContain('Notice 1')
    await wrapper.setProps({ paused: false })
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.text()).toContain('Notice 2')
  })

  it('stops rotating in a background tab and resumes when visible', async () => {
    const wrapper = mountTicker()
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    await nextTick()
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.text()).toContain('Notice 1')
    visibility.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    await nextTick()
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.text()).toContain('Notice 2')
  })

  it('starts paused when the user prefers reduced motion', async () => {
    vi.mocked(window.matchMedia).mockImplementation(query => ({
      matches: query === '(prefers-reduced-motion: reduce)', media: query, onchange: null,
      addListener: vi.fn(), removeListener: vi.fn(),
      addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn(),
    }))
    const wrapper = mountTicker()
    await nextTick()
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.text()).toContain('Notice 1')
  })

  it('opens the displayed notice and resets safely when the list changes', async () => {
    const wrapper = mountTicker()
    await nextTick()
    await vi.advanceTimersByTimeAsync(10000)
    await wrapper.get('[data-testid="announcement-ticker-open"]').trigger('click')
    expect(wrapper.emitted('open')?.[0]).toEqual([notice(3)])
    await wrapper.setProps({ announcements: [notice(4)] })
    expect(wrapper.text()).toContain('Notice 4')
    await vi.advanceTimersByTimeAsync(10000)
    expect(wrapper.text()).toContain('Notice 4')
    await wrapper.setProps({ announcements: [] })
    expect(wrapper.find('button').exists()).toBe(false)
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
