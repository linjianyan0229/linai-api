import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { ReleaseUpdateInfo } from '@/api/admin/system'

const mocks = vi.hoisted(() => ({
  auth: { isAdmin: true }, check: vi.fn(), install: vi.fn(), restart: vi.fn(), getVersion: vi.fn()
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/system', () => ({ checkUpdates: mocks.check, performUpdate: mocks.install, restartService: mocks.restart, getVersion: mocks.getVersion, createSystemOperationKey: () => 'confirmed-operation' }))
import VersionBadge from '../VersionBadge.vue'

let wrapper: VueWrapper | undefined
function info(): ReleaseUpdateInfo {
  return { current_version: '1.9.0', latest_version: '1.11.0', repository: 'linjianyan0229/linai-api', has_update: true, can_update: true, deployment: 'container', checked_at: '2026-10-24T00:00:00Z', releases: ['1.11.0', '1.10.0'].map(version => ({ version, name: `Release ${version}`, notes: '<img src=x onerror=alert(1)>', published_at: '', url: `https://github.com/linjianyan0229/linai-api/releases/tag/v${version}`, can_install: true })) }
}
function render() {
  wrapper = mount(VersionBadge, { props: { version: '1.9.0' }, global: { stubs: {
    BaseDialog: { props: ['show', 'title'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' },
    Select: { props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="item in options" :value="item.value">{{ item.label }}</option></select>' }
  } } })
  return wrapper
}
function button(label: string) {
  const found = wrapper!.findAll('button').find(item => item.text() === label)
  if (!found) throw new Error(`Missing button ${label}`)
  return found
}
beforeEach(() => { vi.clearAllMocks(); mocks.auth.isAdmin = true; mocks.check.mockResolvedValue(info()); mocks.install.mockResolvedValue({ version: '1.10.0', need_restart: true }) })
afterEach(() => wrapper?.unmount())

describe('optional release updates', () => {
  it('automatically checks for an administrator without installing or restarting', async () => {
    render(); await flushPromises()
    expect(mocks.check).toHaveBeenCalledOnce()
    expect(mocks.install).not.toHaveBeenCalled()
    expect(mocks.restart).not.toHaveBeenCalled()
  })
  it('does not call privileged APIs for an ordinary user', async () => {
    mocks.auth.isAdmin = false
    render(); await flushPromises()
    expect(wrapper!.text()).toBe('v1.9.0')
    expect(mocks.check).not.toHaveBeenCalled()
  })
  it('installs the selected version only after explicit confirmation and waits for a separate restart', async () => {
    render(); await flushPromises()
    await wrapper!.get('[aria-label="version.title"]').trigger('click')
    expect(wrapper!.text()).toContain('linjianyan0229/linai-api')
    expect(wrapper!.find('img').exists()).toBe(false)
    await wrapper!.get('select').setValue('1.10.0')
    await button('version.install').trigger('click')
    expect(mocks.install).not.toHaveBeenCalled()
    await button('version.confirmInstall').trigger('click'); await flushPromises()
    expect(mocks.install).toHaveBeenCalledWith('1.10.0', 'confirmed-operation')
    expect(button('version.restart').exists()).toBe(true)
    expect(mocks.restart).not.toHaveBeenCalled()
  })
  it('lets an administrator decline an available update', async () => {
    render(); await flushPromises()
    await wrapper!.get('[aria-label="version.title"]').trigger('click')
    await button('version.later').trigger('click')
    expect(mocks.install).not.toHaveBeenCalled()
  })
  it('disables installation when the selected platform package is missing', async () => {
    const value = info(); value.releases[0].can_install = false; mocks.check.mockResolvedValue(value)
    render(); await flushPromises()
    await wrapper!.get('[aria-label="version.title"]').trigger('click')
    expect(button('version.install').attributes('disabled')).toBeDefined()
    expect(wrapper!.text()).toContain('version.noPackage')
  })
  it('does not label a failed GitHub check as up to date', async () => {
    mocks.check.mockRejectedValue(new Error('offline'))
    render(); await flushPromises()
    await wrapper!.get('[aria-label="version.title"]').trigger('click'); await flushPromises()
    expect(wrapper!.get('[role="alert"]').text()).toBe('version.checkFailed')
    expect(wrapper!.text()).not.toContain('version.latest')
  })
})
