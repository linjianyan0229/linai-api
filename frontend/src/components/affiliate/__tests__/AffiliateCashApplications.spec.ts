import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AffiliateCashApplications from '../AffiliateCashApplications.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), qr: vi.fn(), reply: vi.fn(), retry: vi.fn(), showError: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/affiliateCash', async (original) => ({ ...await original<typeof import('@/api/affiliateCash')>(), affiliateCashAPI: mocks }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
const application = { id: 7, user_id: 42, username: 'Alice', email: 'pay@example.test', amount: '2.00000001', currency: 'USD', status: 'pending', payout_not_before: '2026-10-30T00:00:00Z', created_at: '2026-10-31T00:00:00Z', resolved_at: null, replies: [] }
const wrappers: ReturnType<typeof mount>[] = []
async function setup(admin = true) {
  const wrapper = mount(AffiliateCashApplications, { props: { admin }, global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }, Pagination: true } } })
  wrappers.push(wrapper); await flushPromises()
  await wrapper.get('tbody button').trigger('click'); await flushPromises(); return wrapper
}
beforeEach(() => {
  vi.clearAllMocks(); vi.stubGlobal('crypto', { randomUUID: vi.fn(() => 'reply-request-key') })
  URL.createObjectURL = vi.fn(() => 'blob:protected-payment-qr'); URL.revokeObjectURL = vi.fn()
  mocks.list.mockResolvedValue({ items: [application], total: 1 })
  mocks.get.mockResolvedValue(application); mocks.qr.mockResolvedValue(new Blob(['qr']))
})
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount(); vi.unstubAllGlobals() })

describe('withdrawal applications', () => {
  it('loads private QR through the API and requires payout confirmation before sending', async () => {
    const wrapper = await setup()
    expect(mocks.qr).toHaveBeenCalledWith(7, true)
    expect(wrapper.get('img').attributes('src')).toBe('blob:protected-payment-qr')
    await wrapper.get('[data-testid="cash-decision"]').setValue('paid')
    await wrapper.get('textarea').setValue('Payment completed')
    await wrapper.get('form').trigger('submit')
    expect(mocks.reply).not.toHaveBeenCalled()
    await wrapper.get('input[type="checkbox"]').setValue(true)
    mocks.reply.mockResolvedValue({ ...application, status: 'paid', replies: [{ id: 9, delivery_status: 'failed', content: 'Payment completed', created_at: application.created_at }] })
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.reply).toHaveBeenCalledWith(7, 'Payment completed', 'paid', 'reply-request-key')
    expect(mocks.showError).toHaveBeenCalledWith('affiliate.cash.replySavedMailFailed')
    expect(wrapper.text()).toContain('affiliate.cash.status.paid')
  })
  it('retries the saved reply without submitting a payout action again', async () => {
    const failed = { id: 9, application_id: 7, decision: 'paid', delivery_status: 'failed', content: 'Payment completed', created_at: application.created_at }
    mocks.get.mockResolvedValue({ ...application, status: 'paid', replies: [failed] })
    mocks.retry.mockResolvedValue({ ...application, status: 'paid', replies: [{ ...failed, delivery_status: 'sent' }] })
    const wrapper = await setup()
    const retry = wrapper.findAll('button').find(button => button.text() === 'affiliate.cash.retryMail')!
    await retry.trigger('click'); await flushPromises()
    expect(mocks.retry).toHaveBeenCalledWith(7, 9, false)
    expect(mocks.reply).not.toHaveBeenCalled()
    expect(mocks.showSuccess).toHaveBeenCalledWith('affiliate.cash.replySent')
  })
  it('does not expose admin reply or retry controls in the user view', async () => {
    mocks.get.mockResolvedValue({ ...application, replies: [{ id: 9, content: '<script>text only</script>', delivery_status: 'failed', created_at: application.created_at }] })
    const wrapper = await setup(false)
    expect(mocks.qr).toHaveBeenCalledWith(7, false)
    expect(wrapper.find('form').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('<script>text only</script>')
    expect(wrapper.text()).not.toContain('affiliate.cash.retryMail')
    wrapper.unmount()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:protected-payment-qr')
  })
})
