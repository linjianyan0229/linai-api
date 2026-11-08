import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaFilterBar from '../PlazaFilterBar.vue'
import PlazaGroupSection from '../PlazaGroupSection.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: null }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: true }) }))

function group(id: number, dynamic: boolean): ModelPlazaGroup {
  return {
    id, name: `Group ${id}`, platform: 'openai', description: '', subscription_type: 'standard', is_exclusive: false,
    rate_multiplier: dynamic ? 1 : .5, dynamic_rate_enabled: dynamic,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
    image_rate_independent: false, image_rate_multiplier: 1, video_rate_independent: false, video_rate_multiplier: 1,
    long_context_pricing_enabled: false, models: [{ name: 'gpt-demo', platform: 'openai', pricing: null, official_pricing: null }]
  }
}

describe('model plaza dynamic quote filters', () => {
  it('keeps unavailable quotes out of fixed-price filters and sorts them last', async () => {
    const fixed = group(1, false), unavailable = group(2, true)
    const wrapper = mount(ModelPlazaContent, { props: { response: { description: '', groups: [unavailable, fixed] }, loading: false }, global: { stubs: { PlazaFilterBar: true, PlazaGroupSection: true, Icon: true } } })
    const filters = wrapper.findComponent(PlazaFilterBar)
    expect(filters.props('rates')).toEqual([.5])
    expect(wrapper.findAllComponents(PlazaGroupSection).map(section => section.props('group').id)).toEqual([1, 2])
    filters.vm.$emit('update:rate', .5)
    await wrapper.vm.$nextTick()
    expect(wrapper.findAllComponents(PlazaGroupSection).map(section => section.props('group').id)).toEqual([1])
    await wrapper.setProps({ response: { description: '', groups: [fixed, { ...unavailable, dynamic_rate_min: .16, dynamic_rate_max: .16 }] } })
    expect(filters.props('rates')).toEqual([.16, .5])
  })
})
