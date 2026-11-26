import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { bulkDelete, keysAPI } from '../keys'

const { deleteKey } = vi.hoisted(() => ({ deleteKey: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { delete: deleteKey } }))

describe('API key bulk deletion', () => {
  beforeEach(() => {
    deleteKey.mockReset()
  })

  it('exports bulk deletion and deletes each selected key once in selection order', async () => {
    deleteKey.mockResolvedValue({ data: { message: 'API key deleted successfully' } })

    expect(keysAPI.bulkDelete).toBe(bulkDelete)
    expect(await bulkDelete([3, 1, 3, 2, 1])).toEqual({ succeededIds: [3, 1, 2], failures: [] })
    expect(deleteKey.mock.calls).toEqual([['/keys/3'], ['/keys/1'], ['/keys/2']])
  })

  it('does not send requests when no keys are selected', async () => {
    expect(await bulkDelete([])).toEqual({ succeededIds: [], failures: [] })
    expect(deleteKey).not.toHaveBeenCalled()
  })

  it('preserves partial failures without treating forbidden or missing keys as deleted', async () => {
    const forbidden = { response: { status: 403 }, message: 'Forbidden' }
    const missing = { response: { status: 404 }, message: 'Not found' }
    deleteKey.mockImplementation((url: string) => {
      if (url === '/keys/2') return Promise.reject(forbidden)
      if (url === '/keys/4') return Promise.reject(missing)
      return Promise.resolve({ data: { message: 'API key deleted successfully' } })
    })

    expect(await bulkDelete([1, 2, 3, 4])).toEqual({
      succeededIds: [1, 3],
      failures: [{ id: 2, error: forbidden }, { id: 4, error: missing }]
    })
  })

  it('returns every failure and continues with later batches when all deletions fail', async () => {
    const failure = new Error('network error')
    deleteKey.mockRejectedValue(failure)
    const ids = [1, 2, 3, 4, 5, 6, 7]

    expect(await bulkDelete(ids)).toEqual({
      succeededIds: [],
      failures: ids.map((id) => ({ id, error: failure }))
    })
    expect(deleteKey.mock.calls).toEqual(ids.map((id) => [`/keys/${id}`]))
  })

  it('limits requests to five in flight and continues after a failed batch member', async () => {
    const settle: Array<() => void> = []
    const failure = new Error('network error')
    let inFlight = 0
    let peak = 0
    deleteKey.mockImplementation(() => new Promise((resolve, reject) => {
      inFlight++
      peak = Math.max(peak, inFlight)
      const index = settle.length
      settle.push(() => {
        inFlight--
        if (index === 0) reject(failure)
        else resolve({ data: { message: 'API key deleted successfully' } })
      })
    }))

    const pending = bulkDelete([1, 2, 3, 4, 5, 6, 7])
    expect(deleteKey).toHaveBeenCalledTimes(5)
    settle.slice(0, 4).forEach((finish) => finish())
    await flushPromises()
    expect(deleteKey).toHaveBeenCalledTimes(5)
    settle[4]()
    await flushPromises()
    expect(deleteKey).toHaveBeenCalledTimes(7)
    settle.slice(5).forEach((finish) => finish())

    expect(await pending).toEqual({
      succeededIds: [2, 3, 4, 5, 6, 7],
      failures: [{ id: 1, error: failure }]
    })
    expect(peak).toBe(5)
    expect(inFlight).toBe(0)
  })
})
