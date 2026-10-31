import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AdminOrdersView from '../AdminOrdersView.vue'

const mocks = vi.hoisted(() => ({ getOrders: vi.fn(), app: { cachedPublicSettings: { payment_enabled: true }, publicSettingsLoaded: true, fetchPublicSettings: vi.fn(), showError: vi.fn() } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('@/api/admin/payment', () => ({ adminPaymentAPI: { getOrders: mocks.getOrders }, default: { getOrders: mocks.getOrders } }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))

function render() {
  return mount(AdminOrdersView, { global: {
    directives: { 'liquid-pill': {} },
    stubs: {
      AppLayout: { template: '<main><slot /></main>' },
      AffiliateCashApplications: { template: '<div data-testid="cash-list">withdrawals</div>' },
      BaseDialog: { template: '<div />' }, AdminRefundDialog: true,
      OrderTable: { template: '<div>orders</div>' }, Select: true, Icon: true, Pagination: true
    }
  } })
}
beforeEach(() => {
  vi.clearAllMocks(); mocks.app.cachedPublicSettings.payment_enabled = true
  mocks.getOrders.mockResolvedValue({ data: { items: [], total: 0 } })
})

describe('order management withdrawal tab', () => {
  it('opens the applications list alongside existing payment orders', async () => {
    const wrapper = render(); await flushPromises()
    expect(mocks.getOrders).toHaveBeenCalledOnce()
    await wrapper.get('#order-tab-withdrawals').trigger('click')
    expect(wrapper.find('[data-testid="cash-list"]').exists()).toBe(true)
    expect(wrapper.get('#order-tab-withdrawals').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })
  it('keeps withdrawal management available with online payment disabled', async () => {
    mocks.app.cachedPublicSettings.payment_enabled = false
    const wrapper = render(); await flushPromises()
    expect(mocks.getOrders).not.toHaveBeenCalled()
    expect(wrapper.find('#order-tab-orders').exists()).toBe(false)
    expect(wrapper.find('[data-testid="cash-list"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
