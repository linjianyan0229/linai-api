import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import LeaderboardView from '../LeaderboardView.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/leaderboard', () => ({ getLeaderboard: mocks.get }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ locale: { value: 'en' }, t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))

const snapshot = (model = '', username = 'Alice') => ({ items: [{ rank: 1, username, total_tokens: 1234567, total_quota: '1.23456789' }], models: ['model-a', 'model-b'], model, limit: 20, updated_at: '2026-10-31T00:00:00Z' })
function render() {
  return mount(LeaderboardView, { global: { stubs: {
    AppLayout: { template: '<main><slot /></main>' }, Icon: true,
    Select: { props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="item in options" :key="item.value" :value="item.value">{{ item.label }}</option></select>' },
    DataTable: { props: ['data', 'loading'], template: '<div><p v-if="loading">loading</p><div v-for="row in data" :key="row.rank" data-test="row"><slot name="cell-username" :row="row" /><slot name="cell-total_tokens" :row="row" /><slot name="cell-total_quota" :row="row" /></div><slot v-if="!loading && !data.length" name="empty" /></div>' }
  } } })
}

beforeEach(() => { vi.clearAllMocks(); mocks.get.mockResolvedValue(snapshot()) })
describe('user leaderboard', () => {
  it('loads all models by default and shows the username, exact token count and quota', async () => {
    const wrapper = render(); await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith('', expect.any(AbortSignal))
    expect(wrapper.text()).toContain('TOP 20'); expect(wrapper.text()).toContain('Alice')
    expect(wrapper.text()).toContain('1,234,567'); expect(wrapper.text()).toContain('USD 1.23456789')
    expect(wrapper.get('select').attributes('multiple')).toBeUndefined()
    await wrapper.get('select').setValue('model-a'); await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith('model-a', expect.any(AbortSignal))
    wrapper.unmount()
  })
  it('ignores a stale response after rapidly changing the model', async () => {
    const wrapper = render(); await flushPromises()
    let resolveA!: (value: unknown) => void, resolveB!: (value: unknown) => void
    mocks.get.mockImplementationOnce(() => new Promise(resolve => { resolveA = resolve }))
    mocks.get.mockImplementationOnce(() => new Promise(resolve => { resolveB = resolve }))
    await wrapper.get('select').setValue('model-a')
    await wrapper.get('select').setValue('model-b')
    resolveB(snapshot('model-b', 'Bob')); await flushPromises()
    resolveA(snapshot('model-a', 'Stale Alice')); await flushPromises()
    expect(wrapper.text()).toContain('Bob'); expect(wrapper.text()).not.toContain('Stale Alice')
    wrapper.unmount()
  })
  it('shows empty and failed states and can refresh without reusing stale results', async () => {
    mocks.get.mockResolvedValueOnce({ ...snapshot(), items: [] })
    const wrapper = render(); await flushPromises()
    expect(wrapper.text()).toContain('leaderboard.empty')
    mocks.get.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('leaderboard.unavailable')
    await wrapper.get('button[aria-label="common.refresh"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false); expect(wrapper.text()).toContain('Alice')
    wrapper.unmount()
  })
})
