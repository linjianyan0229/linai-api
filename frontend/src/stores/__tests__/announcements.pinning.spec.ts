import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAnnouncementStore } from '../announcements'
import type { UserAnnouncement } from '@/types'

const { list, markRead } = vi.hoisted(() => ({ list: vi.fn(), markRead: vi.fn() }))
vi.mock('@/api', () => ({ announcementsAPI: { list, markRead } }))

const notice = (id: number): UserAnnouncement => ({
  id, title: `Notice ${id}`, content: 'Content', notify_mode: 'silent',
  created_at: '2026-11-08', updated_at: '2026-11-08',
})

beforeEach(() => {
  setActivePinia(createPinia())
  vi.resetAllMocks()
  vi.useFakeTimers()
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
  list.mockResolvedValue([])
})
afterEach(() => { useAnnouncementStore().$dispose(); vi.restoreAllMocks(); vi.useRealTimers() })

describe('announcement priority and synchronization', () => {
  it('keeps a read pin ahead of unread notices before truncating to twenty', async () => {
    const all = Array.from({ length: 25 }, (_, i) => notice(i + 1))
    all[24] = { ...notice(25), is_pinned: true, read_at: '2026-11-08' }
    list.mockResolvedValue(all)
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    expect(store.announcements).toHaveLength(20)
    expect(store.announcements.map(a => a.id)).toEqual([25, ...Array.from({ length: 19 }, (_, i) => i + 1)])
    expect(store.announcements[0].read_at).toBe('2026-11-08')
    expect(all[0].id).toBe(1)
  })

  it('refreshes visible tabs once per interval and stops on logout', async () => {
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    store.startPolling()
    store.startPolling()
    list.mockResolvedValue([{ ...notice(1), is_pinned: true }])
    await vi.advanceTimersByTimeAsync(30000)
    expect(list).toHaveBeenCalledTimes(2)
    expect(store.announcements[0].is_pinned).toBe(true)
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    await vi.advanceTimersByTimeAsync(60000)
    expect(list).toHaveBeenCalledTimes(2)
    visibility.mockReturnValue('visible')
    await vi.advanceTimersByTimeAsync(30000)
    expect(list).toHaveBeenCalledTimes(3)
    store.reset()
    await vi.advanceTimersByTimeAsync(60000)
    expect(list).toHaveBeenCalledTimes(3)
    expect(store.announcements).toEqual([])
    expect(vi.getTimerCount()).toBe(0)
  })

  it('updates the current popup without replaying it when pin metadata changes', async () => {
    const original = { ...notice(1), notify_mode: 'popup' as const }
    list.mockResolvedValue([original])
    markRead.mockResolvedValue(undefined)
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(1)
    list.mockResolvedValue([{ ...original, is_pinned: true }])
    await store.fetchAnnouncements(true)
    expect(store.currentPopup?.is_pinned).toBe(true)
    list.mockResolvedValue([
      ...Array.from({ length: 25 }, (_, i) => ({ ...notice(i + 2), is_pinned: true })),
      { ...original, is_pinned: false },
    ])
    await store.fetchAnnouncements(true)
    expect(store.currentPopup?.id).toBe(1)
    expect(store.currentPopup?.is_pinned).toBe(false)
    await store.dismissPopup()
    // Even a stale response must not redisplay an already shown popup.
    await store.fetchAnnouncements(true)
    expect(store.currentPopup).toBeNull()
    expect(markRead).toHaveBeenCalledTimes(1)
  })
})
