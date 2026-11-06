import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import GroupTimeRateEditor from '../GroupTimeRateEditor.vue'
import { defaultGroupTimeRates } from '@/utils/groupTimeRates'
import { defaultGroupRechargeRateTiers } from '@/utils/groupRechargeRates'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: object) => `${key} ${JSON.stringify(values ?? {})}` }) }))

describe('GroupTimeRateEditor', () => {
  it('enables a neutral schedule and keeps both periods connected across midnight', async () => {
    const Host = defineComponent({ components: { GroupTimeRateEditor }, setup: () => ({ periods: ref(defaultGroupTimeRates(0.8)) }), template: '<GroupTimeRateEditor v-model="periods" :base-rate="0.8" timezone="Asia/Shanghai" />' })
    const wrapper = mount(Host)
    await wrapper.get('[data-testid="time-start-0"]').setValue('22:00')
    await wrapper.get('[data-testid="time-end-0"]').setValue('06:00')
    expect(wrapper.get<HTMLInputElement>('[data-testid="time-start-1"]').element.value).toBe('06:00')
    expect(wrapper.get<HTMLInputElement>('[data-testid="time-end-1"]').element.value).toBe('22:00')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await wrapper.get('[data-testid="time-rate-1"]').setValue('-1')
    expect(wrapper.get('[role="alert"]').text()).toContain('admin.groups.timeRates.invalid')
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.find('[data-testid="time-rate-0"]').exists()).toBe(false)
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.get<HTMLInputElement>('[data-testid="time-rate-0"]').element.value).toBe('0.8')
    wrapper.unmount()
  })

  it('shows level × time coefficient previews when recharge levels are enabled', async () => {
    const wrapper = mount(GroupTimeRateEditor, { props: { modelValue: [{ start: '08:00', end: '20:00', rate_multiplier: 1.2 }, { start: '20:00', end: '08:00', rate_multiplier: 0.6 }], rechargeTiers: defaultGroupRechargeRateTiers(), baseRate: 0.8, timezone: 'UTC' } })
    await flushPromises()
    expect(wrapper.text()).toContain('"level":2,"peak":0.9,"valley":0.45')
    expect(wrapper.text()).toContain('admin.groups.timeRates.stacked')
    wrapper.unmount()
  })
})
