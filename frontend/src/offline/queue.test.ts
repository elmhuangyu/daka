import { describe, expect, it, vi } from 'vitest'
import { ApiError, type CheckinInput } from '../api'
import { memoryStore, OfflineQueue } from './queue'

function make(send: (i: CheckinInput, k: string) => Promise<unknown>, before = () => {}) {
  const timers: Array<() => void> = []
  const store = memoryStore()
  const q = new OfflineQueue(store, send, before, (fn) => {
    timers.push(fn)
    return timers.length as unknown as ReturnType<typeof setTimeout>
  })
  return { q, store, timers }
}

describe('OfflineQueue', () => {
  it('stores only request fields and replays in order with the original keys', async () => {
    const calls: Array<[CheckinInput, string]> = []
    const { q, store } = make(async (i, k) => void calls.push([i, k]))
    await q.enqueue(
      { item_id: 1, mode: 'set', note: 'n', name: 'Secret name' } as CheckinInput,
      'k1',
    )
    await q.enqueue({ item_id: 2, mode: 'delta', value: 3, date: '2026-01-02' }, 'k2')
    expect(JSON.stringify(await store.all())).not.toContain('Secret name')
    await q.replay()
    expect(calls.map((c) => c[1])).toEqual(['k1', 'k2'])
    expect(calls[1][0]).toEqual({ item_id: 2, mode: 'delta', value: 3, date: '2026-01-02' })
    expect(q.pending).toEqual([])
    expect(await store.all()).toEqual([])
  })

  it('stops at a network failure, keeps order and retries with the same key', async () => {
    let offline = true
    const keys: string[] = []
    const { q, timers } = make(async (_i, k) => {
      keys.push(k)
      if (offline) throw new ApiError('Network error', 0)
    })
    await q.enqueue({ item_id: 1, mode: 'set' }, 'a')
    await q.enqueue({ item_id: 2, mode: 'set' }, 'b')
    await q.replay()
    expect(keys).toEqual(['a'])
    expect(q.pending).toHaveLength(2)
    expect(q.stalled).toBe(true)
    expect(timers).toHaveLength(1)
    offline = false
    timers[0]()
    await q.replay()
    expect(keys).toEqual(['a', 'a', 'b'])
    expect(q.pending).toHaveLength(0)
  })

  it('refreshes the CSRF token before every replay', async () => {
    const before = vi.fn()
    const { q } = make(async () => undefined, before)
    await q.enqueue({ item_id: 1, mode: 'set' }, 'a')
    await q.enqueue({ item_id: 2, mode: 'set' }, 'b')
    await q.replay()
    expect(before).toHaveBeenCalledTimes(2)
  })

  it('drops an entry on a definitive 4xx, reports it and continues', async () => {
    const keys: string[] = []
    const { q } = make(async (_i, k) => {
      keys.push(k)
      if (k === 'bad') throw new ApiError('invalid body', 400, 'invalid body')
    })
    await q.enqueue({ item_id: 1, mode: 'set' }, 'bad')
    await q.enqueue({ item_id: 2, mode: 'set' }, 'ok')
    await q.replay()
    expect(keys).toEqual(['bad', 'ok'])
    expect(q.pending).toHaveLength(0)
    expect(q.failures).toEqual([{ key: 'bad', item_id: 1, message: 'The request was not valid' }])
    q.dismissFailure('bad')
    expect(q.failures).toEqual([])
  })

  it('keeps entries on 5xx and 429', async () => {
    const { q } = make(async () => {
      throw new ApiError('boom', 503)
    })
    await q.enqueue({ item_id: 1, mode: 'set' }, 'a')
    await q.replay()
    expect(q.pending).toHaveLength(1)
    expect(q.failures).toEqual([])
  })

  it('pauses on 401 and on reauth_required until a manual retry', async () => {
    let err: ApiError | null = new ApiError('unauthorized', 401, 'unauthorized')
    const send = vi.fn(async () => {
      if (err) throw err
    })
    const { q } = make(send)
    await q.enqueue({ item_id: 1, mode: 'set' }, 'a')
    await q.replay()
    expect(q.paused).toBe('unauthorized')
    expect(q.pending).toHaveLength(1)
    await q.replay() // still paused: no new request
    expect(send).toHaveBeenCalledTimes(1)
    err = new ApiError('reauth_required', 403, 'reauth_required')
    await q.retry()
    expect(q.paused).toBe('reauth_required')
    err = null
    await q.retry()
    expect(q.paused).toBe('')
    expect(q.pending).toHaveLength(0)
  })

  it('does not double-send when replay is called concurrently', async () => {
    const send = vi.fn(async () => undefined)
    const { q } = make(send)
    await q.enqueue({ item_id: 1, mode: 'set' }, 'a')
    await Promise.all([q.replay(), q.replay()])
    expect(send).toHaveBeenCalledTimes(1)
  })

  it('clear drops queued entries, failures and the persisted rows', async () => {
    const { q, store } = make(async () => {
      throw new ApiError('Network error', 0)
    })
    await q.enqueue({ item_id: 1, mode: 'set' }, 'a')
    await q.replay()
    await q.clear()
    expect(q.pending).toEqual([])
    expect(await store.all()).toEqual([])
    expect(q.stalled).toBe(false)
  })

  it('a clear during a running replay stops it', async () => {
    let release!: () => void
    const send = vi.fn(() => new Promise<void>((r) => (release = r)))
    const { q, store } = make(send)
    await q.enqueue({ item_id: 1, mode: 'set' }, 'a')
    await q.enqueue({ item_id: 2, mode: 'set' }, 'b')
    const run = q.replay()
    await vi.waitFor(() => expect(send).toHaveBeenCalled())
    await q.clear()
    release()
    await run
    expect(send).toHaveBeenCalledTimes(1)
    expect(await store.all()).toEqual([])
  })

  it('refuses ad-hoc entries (their note is plaintext)', async () => {
    const { q, store } = make(async () => undefined)
    await expect(
      q.enqueue({ item_id: 1, mode: 'append', adhoc: true, note: 'Name' }, 'a'),
    ).rejects.toThrow()
    expect(await store.all()).toEqual([])
  })

  it('scheduleRetry arms a single timer that replays', async () => {
    const send = vi.fn(async () => undefined)
    const { q, timers } = make(send)
    await q.enqueue({ item_id: 1, mode: 'set' }, 'a')
    q.scheduleRetry()
    q.scheduleRetry()
    expect(timers).toHaveLength(1)
    timers[0]()
    await vi.waitFor(() => expect(send).toHaveBeenCalledTimes(1))
  })
})
