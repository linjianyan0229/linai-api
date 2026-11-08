import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import GroupDynamicRateEditor from '../GroupDynamicRateEditor.vue'
import { getDynamicRateAvailability } from '@/api/admin/groups'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, args?: unknown) => `${key} ${args ? JSON.stringify(args) : ''}` }) }))
vi.mock('@/api/admin/groups', () => ({ getDynamicRateAvailability: vi.fn() }))

describe('GroupDynamicRateEditor', () => {
  beforeEach(() => vi.mocked(getDynamicRateAvailability).mockReset())

  it('requires an existing group with a valid upstream quote', async () => {
    const create = mount(GroupDynamicRateEditor, { props: { enabled: false, factor: 2 } })
    expect(create.get('[role="switch"]').attributes('disabled')).toBeDefined()
    expect(getDynamicRateAvailability).not.toHaveBeenCalled()
    create.unmount()

    vi.mocked(getDynamicRateAvailability).mockResolvedValue({ eligible_accounts: 0, upstream_min: null, upstream_max: null })
    const edit = mount(GroupDynamicRateEditor, { props: { enabled: false, factor: 2, groupId: 7 } })
    await flushPromises()
    expect(edit.get('[role="switch"]').attributes('disabled')).toBeDefined()
    await edit.get('[role="switch"]').trigger('click')
    expect(edit.emitted('update:enabled')).toBeUndefined()
  })

  it('previews the real upstream range times the factor and allows disabling an unavailable group', async () => {
    vi.mocked(getDynamicRateAvailability).mockResolvedValue({ eligible_accounts: 2, upstream_min: .08, upstream_max: .1 })
    const wrapper = mount(GroupDynamicRateEditor, { props: { enabled: true, factor: 2, groupId: 7 } })
    await flushPromises()
    expect(wrapper.get('[data-testid="dynamic-rate-preview"]').text()).toContain('0.16–0.2×')
    await wrapper.setProps({ factor: 3 })
    expect(wrapper.get('[data-testid="dynamic-rate-preview"]').text()).toContain('0.24–0.3×')
    vi.mocked(getDynamicRateAvailability).mockResolvedValue({ eligible_accounts: 0, upstream_min: null, upstream_max: null })
    await wrapper.findAll('button').find(button => button.text().includes('common.refresh'))!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:enabled')).toEqual([[false]])
  })

  it('ignores an old response after switching groups', async () => {
    let resolveFirst!: (value: Awaited<ReturnType<typeof getDynamicRateAvailability>>) => void
    vi.mocked(getDynamicRateAvailability).mockImplementationOnce(() => new Promise(resolve => { resolveFirst = resolve }))
    const wrapper = mount(GroupDynamicRateEditor, { props: { enabled: false, factor: 2, groupId: 7 } })
    vi.mocked(getDynamicRateAvailability).mockResolvedValue({ eligible_accounts: 0, upstream_min: null, upstream_max: null })
    await wrapper.setProps({ groupId: 8 })
    await flushPromises()
    resolveFirst({ eligible_accounts: 1, upstream_min: .08, upstream_max: .08 })
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('disabled')).toBeDefined()
  })
})
