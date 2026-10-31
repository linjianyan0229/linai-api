import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProfileAnnouncementNotifyCard from '../ProfileAnnouncementNotifyCard.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn(), update: vi.fn(), success: vi.fn() }))
vi.mock('@/api/user', () => ({ getAnnouncementEmailPreference: mocks.get, updateAnnouncementEmailPreference: mocks.update }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.success }) }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue({ enabled: false, available: true, email: 'user@example.test' })
  mocks.update.mockImplementation((enabled: boolean) => Promise.resolve({ enabled, available: true, email: 'user@example.test' }))
})

describe('announcement email subscription', () => {
  it('shows the stored preference and persists opt-in and opt-out', async () => {
    const wrapper = mount(ProfileAnnouncementNotifyCard, { props: { userEmail: 'user@example.test' } })
    await flushPromises()
    const toggle = wrapper.get('[role="switch"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await toggle.trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenLastCalledWith(true)
    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(mocks.success).toHaveBeenCalledWith('profile.announcementEmail.savedEnabled')
    await toggle.trigger('click'); await flushPromises()
    expect(mocks.update).toHaveBeenLastCalledWith(false)
    expect(toggle.attributes('aria-checked')).toBe('false')
    wrapper.unmount()
  })
  it('keeps the old switch state when a save fails', async () => {
    mocks.update.mockRejectedValue(new Error('SMTP preference unavailable'))
    const wrapper = mount(ProfileAnnouncementNotifyCard, { props: { userEmail: 'user@example.test' } })
    await flushPromises(); await wrapper.get('[role="switch"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[role="alert"]').exists()).toBe(true)
    expect(mocks.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('prevents enabling mail for an unbound OAuth placeholder address but permits disabling', async () => {
    mocks.get.mockResolvedValueOnce({ enabled: false, available: false, email: 'id@oidc-connect.invalid' })
    const wrapper = mount(ProfileAnnouncementNotifyCard, { props: { userEmail: 'id@oidc-connect.invalid' } })
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    mocks.get.mockResolvedValueOnce({ enabled: true, available: false, email: 'id@oidc-connect.invalid' })
    await wrapper.setProps({ userEmail: 'other@oidc-connect.invalid' }); await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
  it('shows a retry when preferences cannot load and disables duplicate saves', async () => {
    mocks.get.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mount(ProfileAnnouncementNotifyCard, { props: { userEmail: 'user@example.test' } })
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[role="alert"] button').trigger('click'); await flushPromises()
    let finish!: (value: unknown) => void
    mocks.update.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(mocks.update).toHaveBeenCalledTimes(1)
    finish({ enabled: true, available: true, email: 'user@example.test' }); await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })
})
