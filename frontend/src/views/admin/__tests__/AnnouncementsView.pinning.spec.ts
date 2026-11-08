import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AnnouncementsView from '../AnnouncementsView.vue'
import { useAnnouncementStore } from '@/stores/announcements'
import type { Announcement } from '@/types'

const { adminList, userList, setPinned, showSuccess, showError } = vi.hoisted(() => ({
  adminList: vi.fn(), userList: vi.fn(), setPinned: vi.fn(), showSuccess: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: { announcements: { list: adminList, setPinned }, groups: { getAll: async () => [] } } }))
vi.mock('@/api', () => ({ announcementsAPI: { list: userList } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

let records: Announcement[]
enableAutoUnmount(afterEach)
beforeEach(() => {
  setActivePinia(createPinia())
  vi.resetAllMocks()
  records = [2, 1].map(id => ({ id, title: `Notice ${id}`, content: 'Content', status: 'active', notify_mode: 'silent',
    is_pinned: false, targeting: {}, created_at: '2026-11-08', updated_at: '2026-11-08' }))
  const ordered = () => records.map(a => ({ ...a })).sort((a, b) => Number(b.is_pinned) - Number(a.is_pinned))
  adminList.mockImplementation(async () => ({ items: ordered(), page: 1, page_size: 20, total: 2, pages: 1 }))
  userList.mockImplementation(async () => ordered())
  setPinned.mockImplementation(async (id, pinned) => { records.find(a => a.id === id)!.is_pinned = pinned })
})

const mountPage = () => mount(AnnouncementsView, { global: { stubs: {
  AppLayout: { template: '<main><slot /></main>' },
  TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
  BaseDialog: true, ConfirmDialog: true, Pagination: true, Select: true, Icon: true,
  AnnouncementTargetingEditor: true, AnnouncementReadStatusDialog: true, AnnouncementPopup: true,
} } })

describe('admin announcement pin action', () => {
  it('pins and unpins a row, refreshing the shared display despite its throttle', async () => {
    const store = useAnnouncementStore()
    await store.fetchAnnouncements()
    const wrapper = mountPage()
    await flushPromises()
    const button = () => wrapper.get('[data-testid="pin-announcement-1"]')
    await button().trigger('click')
    await flushPromises()
    expect(setPinned).toHaveBeenLastCalledWith(1, true)
    expect(userList).toHaveBeenCalledTimes(2)
    expect(store.announcements[0].id).toBe(1)
    expect(button().attributes('aria-pressed')).toBe('true')
    await button().trigger('click')
    await flushPromises()
    expect(setPinned).toHaveBeenLastCalledWith(1, false)
    expect(store.announcements[0].id).toBe(2)
    expect(showSuccess).toHaveBeenLastCalledWith('admin.announcements.unpinSuccess')
  })

  it('does not change the display or keep the button locked after a failed save', async () => {
    const wrapper = mountPage()
    await flushPromises()
    setPinned.mockRejectedValue(new Error('offline'))
    await wrapper.get('[data-testid="pin-announcement-1"]').trigger('click')
    await flushPromises()
    expect(userList).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.announcements.failedToPin')
    expect(wrapper.get('[data-testid="pin-announcement-1"]').attributes('disabled')).toBeUndefined()
  })
})
