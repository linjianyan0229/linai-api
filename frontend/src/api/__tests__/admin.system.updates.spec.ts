import { beforeEach, describe, expect, it, vi } from 'vitest'
const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post } }))
import { checkUpdates, performUpdate, restartService } from '@/api/admin/system'

beforeEach(() => { vi.clearAllMocks(); get.mockResolvedValue({ data: {} }); post.mockResolvedValue({ data: { version: '1.2.3', need_restart: true } }) })
describe('system release operations', () => {
  it('keeps detection read only and sends an explicit version with its retry key when installing', async () => {
    await checkUpdates(true)
    expect(get).toHaveBeenCalledWith('/admin/system/check-updates', { params: { force: true } })
    expect(post).not.toHaveBeenCalled()
    await performUpdate('1.2.3', 'one-confirmed-operation')
    await performUpdate('1.2.3', 'one-confirmed-operation')
    expect(post).toHaveBeenLastCalledWith('/admin/system/update', { version: '1.2.3' }, { timeout: 900000, headers: { 'Idempotency-Key': 'one-confirmed-operation' } })
  })
  it('gives a separately requested restart an idempotency key', async () => {
    await restartService()
    expect(post).toHaveBeenCalledWith('/admin/system/restart', undefined, { headers: { 'Idempotency-Key': expect.stringMatching(/^system-restart-/) } })
  })
})
