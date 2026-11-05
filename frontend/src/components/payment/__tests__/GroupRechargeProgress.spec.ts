import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import GroupRechargeProgress from '../GroupRechargeProgress.vue'
import { getRechargeProgress } from '@/api/groups'
import { defaultGroupRechargeRateTiers } from '@/utils/groupRechargeRates'

vi.mock('@/api/groups', () => ({ getRechargeProgress: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: object) => `${key} ${JSON.stringify(values ?? {})}` }) }))

describe('GroupRechargeProgress', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows the earned level and the remaining USD principal, including a manual override', async () => {
    vi.mocked(getRechargeProgress).mockResolvedValue({
      principal_usd: 125,
      groups: [{ group_id: 1, group_name: 'OpenAI', tiers: defaultGroupRechargeRateTiers(), level: 2, rate_multiplier: 0.6, has_user_override: true }]
    })
    const wrapper = mount(GroupRechargeProgress)
    await flushPromises()
    expect(wrapper.text()).toContain('$125.00')
    expect(wrapper.text()).toContain('"amount":"375.00"')
    expect(wrapper.text()).toContain('"rate":0.6')
    expect(wrapper.text()).toContain('payment.groupRates.override')
    expect(wrapper.get('[aria-current="step"]').text()).toContain('0.75×')
    wrapper.unmount()
  })

  it('hides when no available group enables recharge levels', async () => {
    vi.mocked(getRechargeProgress).mockResolvedValue({ principal_usd: 10, groups: [] })
    const wrapper = mount(GroupRechargeProgress)
    await flushPromises()
    expect(wrapper.find('section').exists()).toBe(false)
    wrapper.unmount()
  })

  it('retries a failed load and displays the highest level', async () => {
    vi.mocked(getRechargeProgress).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({
      principal_usd: 500,
      groups: [{ group_id: 1, group_name: 'OpenAI', tiers: defaultGroupRechargeRateTiers(), level: 3, rate_multiplier: 0.5, has_user_override: false }]
    })
    const wrapper = mount(GroupRechargeProgress)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('payment.groupRates.loadError')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('payment.groupRates.highest')
    expect(wrapper.text()).not.toContain('payment.groupRates.override')
    wrapper.unmount()
  })
})
