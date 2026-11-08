import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GroupBadge from '../GroupBadge.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: {} }) }))

describe('GroupBadge dynamic rates', () => {
  it('displays the quoted rate and suppresses dormant overrides and peak badges', async () => {
    const wrapper = mount(GroupBadge, { props: { name: 'Dynamic', rateMultiplier: 1, userRateMultiplier: .5, dynamicRateEnabled: true, dynamicRateMin: .16, dynamicRateMax: .16, peakRateEnabled: true, peakStart: '08:00', peakEnd: '20:00', peakRateMultiplier: 3 } })
    expect(wrapper.text()).toContain('0.16×')
    expect(wrapper.text()).not.toContain('0.5x')
    expect(wrapper.text()).not.toContain('08:00')
    await wrapper.setProps({ dynamicRateMin: .2, dynamicRateMax: .2 })
    expect(wrapper.text()).toContain('0.2×')
    await wrapper.setProps({ dynamicRateEnabled: false })
    expect(wrapper.text()).toContain('0.5x')
  })
})
