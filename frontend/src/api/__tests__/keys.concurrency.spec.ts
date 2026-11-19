import { beforeEach, describe, expect, it, vi } from 'vitest'
import { create, update } from '../keys'

const { post, put } = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { post, put } }))

describe('API key concurrency payloads', () => {
  beforeEach(() => {
    post.mockReset().mockResolvedValue({ data: { id: 1 } })
    put.mockReset().mockResolvedValue({ data: { id: 1 } })
  })

  it('omits an unspecified create cap so the server default applies', async () => {
    await create('Key')
    expect(post).toHaveBeenCalledWith('/keys', { name: 'Key' })
  })

  it.each([0, 7, 100000])('sends an explicitly chosen create cap of %i', async (concurrency) => {
    await create('Key', undefined, undefined, undefined, undefined, undefined, undefined, undefined, concurrency)
    expect(post).toHaveBeenCalledWith('/keys', { name: 'Key', concurrency })
  })

  it('preserves an existing cap when the update omits concurrency', async () => {
    await update(1, { name: 'Renamed' })
    expect(put).toHaveBeenCalledWith('/keys/1', { name: 'Renamed' })
  })

  it.each([0, 7, 100000])('sends an explicitly chosen update cap of %i, including clear', async (concurrency) => {
    await update(1, { concurrency })
    expect(put).toHaveBeenCalledWith('/keys/1', { concurrency })
  })
})
