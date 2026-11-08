import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import * as api from '../api'
import { useOfflineStore } from './offline'
import { useTodayStore } from './today'

describe('offline store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('queues a check-in on a network error and not on other errors', async () => {
    const store = useOfflineStore()
    await store.bindUser('u1')
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
    await store.bindUser('u1')
    const post = vi.spyOn(api, 'postCheckin')
    post.mockRejectedValueOnce(new api.ApiError('Network error', 0))
    await store.checkin({ item_id: 5, mode: 'set' }, 'orig')
    post.mockResolvedValue(undefined)
    await store.replay()
    expect(post).toHaveBeenLastCalledWith({ item_id: 5, mode: 'set', date: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/) }, 'orig')
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

  it('stamps the local date at enqueue, in the Today view timezone', async () => {
    const store = useOfflineStore()
    await store.bindUser('u1')
    useTodayStore().view = { date: '2026-01-01', weekday: 4, timezone: 'Pacific/Kiritimati', progress: { done: 0, total: 0 }, items: [] }
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-03-10T11:00:00Z')) // already 2026-03-11 in UTC+14
    const post = vi.spyOn(api, 'postCheckin').mockRejectedValue(new api.ApiError('Network error', 0))
    await store.checkin({ item_id: 1, mode: 'set' }, 'k')
    await store.checkin({ item_id: 2, mode: 'set', date: '2026-03-01' }, 'k2')
    post.mockResolvedValue(undefined)
    await store.replay()
    vi.useRealTimers()
    expect(post).toHaveBeenCalledWith({ item_id: 1, mode: 'set', date: '2026-03-11' }, 'k')
    expect(post).toHaveBeenCalledWith({ item_id: 2, mode: 'set', date: '2026-03-01' }, 'k2')
  })

  it('never replays before the identity is bound', async () => {
    const store = useOfflineStore()
    const post = vi.spyOn(api, 'postCheckin').mockRejectedValueOnce(new api.ApiError('Network error', 0))
    await store.checkin({ item_id: 1, mode: 'set' }, 'k')
    post.mockClear()
    post.mockResolvedValue(undefined)
    await store.replay()
    await store.retry()
    expect(post).not.toHaveBeenCalled()
    await store.bindUser('u1') // binding triggers the first replay
    await vi.waitFor(() => expect(post).toHaveBeenCalledTimes(1))
  })

  it('start() waits for auth and does not replay when signed out', async () => {
    const store = useOfflineStore()
    vi.spyOn(api, 'getAuthStatus').mockResolvedValue({ oidc_enabled: false, login_required: true, authenticated: false })
    const post = vi.spyOn(api, 'postCheckin').mockRejectedValueOnce(new api.ApiError('Network error', 0))
    await store.checkin({ item_id: 1, mode: 'set' }, 'k')
    post.mockClear()
    await store.start()
    expect(post).not.toHaveBeenCalled()
    expect(store.bound).toBe(false)
  })

  it('refuses ad-hoc, sensitive-item and unknown-item notes offline', async () => {
    const store = useOfflineStore()
    await store.bindUser('u1')
    vi.spyOn(api, 'postCheckin').mockRejectedValue(new api.ApiError('Network error', 0))
    await expect(store.checkin({ item_id: 1, mode: 'append', adhoc: true, note: 'Secret option', value: 1 }, 'a')).rejects.toThrow('needs a connection')
    await expect(store.checkin({ item_id: 1, mode: 'set', note: 'x' }, 'b')).rejects.toThrow('needs a connection') // unknown item
    useTodayStore().items = [{ id: 2, effective_sensitive: true }, { id: 3, effective_sensitive: false }] as never
    await expect(store.checkin({ item_id: 2, mode: 'set', note: 'x' }, 'c')).rejects.toThrow('needs a connection')
    expect(store.pendingCount).toBe(0)
    expect((await store.checkin({ item_id: 2, mode: 'set' }, 'd')).queued).toBe(true) // no free text: fine
    expect((await store.checkin({ item_id: 3, mode: 'set', note: 'ok' }, 'e')).queued).toBe(true)
    expect(store.pendingCount).toBe(2)
  })
})
