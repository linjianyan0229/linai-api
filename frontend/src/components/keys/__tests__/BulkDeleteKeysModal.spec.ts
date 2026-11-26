import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BulkDeleteKeysModal from '../BulkDeleteKeysModal.vue'

const { bulkDelete, showSuccess, showError } = vi.hoisted(() => ({
  bulkDelete: vi.fn(), showSuccess: vi.fn(), showError: vi.fn()
}))
vi.mock('@/api', () => ({ keysAPI: { bulkDelete } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => `${key} ${JSON.stringify(params ?? {})}` })
}))

const mountModal = (selectedKeys = [{ id: 1, name: 'First' }, { id: 2, name: 'Second' }]) => mount(BulkDeleteKeysModal, {
  props: { show: true, selectedKeys },
  global: {
    stubs: {
      BaseDialog: {
        name: 'BaseDialog',
        props: ['show', 'closeOnEscape', 'showCloseButton'],
        emits: ['close'],
        template: '<div v-if="show"><slot /><slot name="footer" /></div>'
      }
    }
  }
})

describe('BulkDeleteKeysModal', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    bulkDelete.mockResolvedValue({ succeededIds: [1, 2], failures: [] })
  })

  it('requires confirmation and cancels without deleting keys', async () => {
    const wrapper = mountModal()
    expect(wrapper.text()).toContain('keys.bulkDelete.confirmMessage {"count":2}')
    expect(wrapper.text()).toContain('#1 First')
    expect(wrapper.text()).toContain('#2 Second')
    expect(bulkDelete).not.toHaveBeenCalled()
    await wrapper.get('[data-test="cancel"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('deleted')).toBeUndefined()
    expect(bulkDelete).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('deletes only the reviewed selection and reports successful IDs', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ selectedKeys: [{ id: 3, name: 'Changed selection' }] })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(bulkDelete).toHaveBeenCalledTimes(1)
    expect(bulkDelete).toHaveBeenCalledWith([1, 2])
    expect(wrapper.emitted('deleted')).toEqual([[[1, 2]]])
    expect(showSuccess).toHaveBeenCalledWith('keys.bulkDelete.success {"count":2}')
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps partial failures open and retries only failed keys', async () => {
    bulkDelete.mockResolvedValueOnce({
      succeededIds: [1], failures: [{ id: 2, error: { message: 'Delete denied' } }]
    }).mockResolvedValueOnce({ succeededIds: [2], failures: [] })
    const wrapper = mountModal()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('#2 Second: Delete denied')
    expect(wrapper.text()).toContain('keys.bulkDelete.confirmMessage {"count":1}')
    expect(wrapper.emitted('deleted')).toEqual([[[1]]])
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(showError).toHaveBeenCalledWith('keys.bulkDelete.partialFailure {"success":1,"failed":1}')
    expect(showSuccess).not.toHaveBeenCalled()
    await wrapper.setProps({ selectedKeys: [{ id: 2, name: 'Second' }, { id: 3, name: 'New selection' }] })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(bulkDelete).toHaveBeenLastCalledWith([2])
    expect(wrapper.emitted('deleted')).toEqual([[[1]], [[2]]])
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps all failed keys selected without reporting success', async () => {
    bulkDelete.mockResolvedValueOnce({
      succeededIds: [], failures: [{ id: 1, error: {} }, { id: 2, error: null }]
    })
    const wrapper = mountModal()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('keys.failedToDelete')
    expect(wrapper.emitted('deleted')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('blocks duplicate requests, cancellation and closing while deleting', async () => {
    let finish!: (result: { succeededIds: number[]; failures: [] }) => void
    bulkDelete.mockReturnValue(new Promise((resolve) => { finish = resolve }))
    const wrapper = mountModal()
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('[data-test="cancel"]').trigger('click')
    const dialog = wrapper.findComponent({ name: 'BaseDialog' })
    dialog.vm.$emit('close')
    expect(bulkDelete).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(dialog.props('closeOnEscape')).toBe(false)
    expect(dialog.props('showCloseButton')).toBe(false)
    expect(wrapper.get('[data-test="submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="cancel"]').attributes('disabled')).toBeDefined()
    finish({ succeededIds: [1, 2], failures: [] })
    await flushPromises()
    wrapper.unmount()
  })

  it('preserves pending keys after an unexpected request error', async () => {
    bulkDelete.mockRejectedValueOnce(new Error('Network unavailable'))
    const wrapper = mountModal()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('Network unavailable')
    expect(wrapper.emitted('deleted')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    expect(bulkDelete).toHaveBeenLastCalledWith([1, 2])
    await flushPromises()
    wrapper.unmount()
  })

  it('does not delete an empty selection and captures new keys when reopened', async () => {
    const wrapper = mountModal([])
    expect(wrapper.get('[data-test="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(bulkDelete).not.toHaveBeenCalled()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, selectedKeys: [{ id: 3, name: 'Third' }] })
    expect(wrapper.text()).toContain('#3 Third')
    bulkDelete.mockResolvedValueOnce({ succeededIds: [3], failures: [] })
    await wrapper.get('form').trigger('submit')
    expect(bulkDelete).toHaveBeenCalledWith([3])
    await flushPromises()
    expect(wrapper.emitted('deleted')).toEqual([[[3]]])
    wrapper.unmount()
  })
})
