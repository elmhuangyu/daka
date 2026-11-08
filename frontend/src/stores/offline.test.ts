import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import * as api from '../api'
import { useOfflineStore } from './offline'

describe('offline store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('queues a check-in on a network error and not on other errors', async () => {
    const store = useOfflineStore()
    const post = vi.spyOn(api, 'postCheckin')
    post.mockRejectedValueOnce(new api.ApiError('Network error', 0))
    expect(await store.checkin({ item_id: 5, mode: 'set' }, 'k')).toEqual({ queued: true })
    expect(store.pendingCount).toBe(1)
    expect(store.pendingItems).toEqual([5])
    post.mockRejectedValueOnce(new api.ApiError('bad', 400))
    await expect(store.checkin({ item_id: 6, mode: 'set' }, 'k2')).rejects.toThrow('bad')
    expect(store.pendingCount).toBe(1)
    post.mockResolvedValueOnce(undefined)
    expect(await store.checkin({ item_id: 7, mode: 'set' }, 'k3')).toEqual({ queued: false })
  })

  it('replays through postCheckin with the original key and clears on logout', async () => {
    const store = useOfflineStore()
    const post = vi.spyOn(api, 'postCheckin')
    post.mockRejectedValueOnce(new api.ApiError('Network error', 0))
    await store.checkin({ item_id: 5, mode: 'set' }, 'orig')
    post.mockResolvedValue(undefined)
    await store.replay()
    expect(post).toHaveBeenLastCalledWith({ item_id: 5, mode: 'set' }, 'orig')
    expect(store.pendingCount).toBe(0)

    post.mockRejectedValueOnce(new api.ApiError('Network error', 0))
    await store.checkin({ item_id: 8, mode: 'set' }, 'k')
    expect(store.pendingCount).toBe(1)
    await store.clear()
    expect(store.pendingCount).toBe(0)
  })

  it('drops the queue when a different user signs in', async () => {
    const store = useOfflineStore()
    vi.spyOn(api, 'postCheckin').mockRejectedValueOnce(new api.ApiError('Network error', 0))
    await store.bindUser('u1')
    await store.checkin({ item_id: 1, mode: 'set' }, 'k')
    await store.bindUser('u1')
    expect(store.pendingCount).toBe(1)
    await store.bindUser('u2')
    expect(store.pendingCount).toBe(0)
    expect(localStorage.getItem('daka.offline.user')).toBe('u2')
  })
})
