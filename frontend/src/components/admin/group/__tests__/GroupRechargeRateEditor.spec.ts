import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GroupRechargeRateEditor from '../GroupRechargeRateEditor.vue'
import { defaultGroupRechargeRateTiers, validGroupRechargeRateTiers } from '@/utils/groupRechargeRates'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('GroupRechargeRateEditor', () => {
  it('enables exactly three editable levels and can disable them', async () => {
    const wrapper = mount(GroupRechargeRateEditor, { props: { modelValue: [] } })
    expect(wrapper.findAll('input')).toHaveLength(0)
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual(defaultGroupRechargeRateTiers())
    await wrapper.setProps({ modelValue: defaultGroupRechargeRateTiers() })
    expect(wrapper.findAll('input')).toHaveLength(6)
    expect(wrapper.get('[data-testid="recharge-threshold-0"]').attributes()).toHaveProperty('readonly')
    await wrapper.get('[data-testid="recharge-threshold-1"]').setValue(200)
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([
      { min_amount: 0, rate_multiplier: 1 },
      { min_amount: 200, rate_multiplier: 0.75 },
      { min_amount: 500, rate_multiplier: 0.5 }
    ])
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([])
    wrapper.unmount()
  })

  it('rejects empty, unordered, duplicate or overly precise enabled levels', async () => {
    expect(validGroupRechargeRateTiers([])).toBe(true)
    expect(validGroupRechargeRateTiers(defaultGroupRechargeRateTiers())).toBe(true)
    for (const [threshold, rate] of [[NaN, 0.75], [500, 0.75], [100.001, 0.75], [100, 1], [100, 0.4], [100, 0.75001]]) {
      const tiers = defaultGroupRechargeRateTiers()
      tiers[1] = { min_amount: threshold!, rate_multiplier: rate! }
      expect(validGroupRechargeRateTiers(tiers)).toBe(false)
    }
    const invalid = defaultGroupRechargeRateTiers()
    invalid[1]!.min_amount = 500
    const wrapper = mount(GroupRechargeRateEditor, { props: { modelValue: invalid } })
    expect(wrapper.get('[role="alert"]').text()).toBe('admin.groups.rechargeRates.invalid')
    wrapper.unmount()
  })
})
