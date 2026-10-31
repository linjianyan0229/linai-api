import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AffiliateCashPanel from '../AffiliateCashPanel.vue'
import { cashAmountUnits } from '@/api/affiliateCash'

const mocks = vi.hoisted(() => ({ summary: vi.fn(), apply: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/affiliateCash', async (original) => ({ ...await original<typeof import('@/api/affiliateCash')>(), affiliateCashAPI: { summary: mocks.summary, apply: mocks.apply } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { email: 'owner@example.test' } }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
const stubs = {
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  AffiliateCashApplications: { template: '<div />' }
}
const wrappers: ReturnType<typeof mount>[] = []
async function setup() {
  const wrapper = mount(AffiliateCashPanel, { global: { stubs } })
  wrappers.push(wrapper); await flushPromises(); return wrapper
}
async function file(wrapper: ReturnType<typeof mount>) {
  const input = wrapper.get('[data-testid="cash-qr"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['qr'], 'pay.png', { type: 'image/png', lastModified: 1 })] })
  await input.trigger('change')
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('crypto', { randomUUID: vi.fn().mockReturnValueOnce('first-request-key').mockReturnValue('next-request-key') })
  URL.createObjectURL = vi.fn(() => 'blob:private-preview')
  URL.revokeObjectURL = vi.fn()
  mocks.summary.mockResolvedValue({ eligible_amount: '5.00000001', pending_amount: '2.00000000', next_expires_at: null, server_time: new Date().toISOString() })
  mocks.apply.mockResolvedValue({ id: 1 })
})
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount(); vi.unstubAllGlobals(); vi.useRealTimers() })

describe('cash application', () => {
  it('submits the contact email, exact USD string and payment file and refreshes quota', async () => {
    const wrapper = await setup()
    await wrapper.get('button').trigger('click')
    expect((wrapper.get('[data-testid="cash-email"]').element as HTMLInputElement).value).toBe('owner@example.test')
    await wrapper.get('[data-testid="cash-amount"]').setValue('0.00000001'); await file(wrapper)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.apply).toHaveBeenCalledWith('owner@example.test', '0.00000001', expect.any(File), 'first-request-key')
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(mocks.summary).toHaveBeenCalledTimes(2)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:private-preview')
  })
  it('keeps the same idempotency key when retrying an uncertain request', async () => {
    mocks.apply.mockRejectedValueOnce(new Error('network timeout'))
    const wrapper = await setup(); await wrapper.get('button').trigger('click')
    await wrapper.get('[data-testid="cash-amount"]').setValue('1'); await file(wrapper)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.apply.mock.calls.map(call => call[3])).toEqual(['first-request-key', 'first-request-key'])
  })
  it('rejects excessive precision and overspending without sending the request', async () => {
    const wrapper = await setup(); await wrapper.get('button').trigger('click'); await file(wrapper)
    for (const value of ['0.000000001', '5.00000002', '1e3', '-1']) {
      await wrapper.get('[data-testid="cash-amount"]').setValue(value)
      await wrapper.get('form').trigger('submit')
    }
    expect(mocks.apply).not.toHaveBeenCalled()
    expect(cashAmountUnits('5.00000001')).toBe(500000001n)
  })
  it('refreshes eligibility when the earliest credit expires using the server clock', async () => {
    vi.useFakeTimers()
    mocks.summary.mockResolvedValueOnce({ eligible_amount: '1', pending_amount: '0', next_expires_at: '2026-10-31T01:00:01Z', server_time: '2026-10-31T01:00:00Z' })
    mocks.summary.mockResolvedValue({ eligible_amount: '0', pending_amount: '0', next_expires_at: null, server_time: '2026-10-31T01:00:02Z' })
    const wrapper = await setup()
    await vi.advanceTimersByTimeAsync(1100); await flushPromises()
    expect(mocks.summary).toHaveBeenCalledTimes(2)
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })
})
