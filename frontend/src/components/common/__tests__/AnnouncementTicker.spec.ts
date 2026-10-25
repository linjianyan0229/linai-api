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
