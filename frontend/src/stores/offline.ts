import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import * as api from '../api'
import type { CheckinInput } from '../api'
import { idbStore, OfflineQueue, type PauseReason, type QueueFailure } from '../offline/queue'

const USER_KEY = 'daka.offline.user'

/** Result of an offline-aware check-in. `queued` means it is stored locally, not yet confirmed. */
export interface SubmitResult {
  queued: boolean
}

export const useOfflineStore = defineStore('offline', () => {
  let queue: OfflineQueue | null = null
  const online = ref(typeof navigator === 'undefined' ? true : navigator.onLine)
  const pendingCount = ref(0)
  const pendingItems = ref<number[]>([])
  const failures = ref<QueueFailure[]>([])
  const paused = ref<PauseReason>('')
  const stalled = ref(false)
  const replaying = ref(false)
  /** Bumped after a replay that confirmed at least one action, so views can refresh. */
  const syncedTick = ref(0)

  function q(): OfflineQueue {
    if (!queue) {
      queue = new OfflineQueue(idbStore(), api.postCheckin, api.resetCsrf)
      queue.onChange = sync
    }
    return queue
  }

  function sync() {
    const cur = q()
    const before = pendingCount.value
    pendingCount.value = cur.pending.length
    pendingItems.value = [...cur.pendingItemIds()]
    failures.value = [...cur.failures]
    paused.value = cur.paused
    stalled.value = cur.stalled
    if (cur.pending.length < before) syncedTick.value++
  }

  /** Loads persisted entries and replays them (app start). */
  async function start() {
    await q().load()
    sync()
    if (pendingCount.value > 0) void replay()
  }

  async function replay() {
    replaying.value = true
    try {
      await q().replay()
    } finally {
      replaying.value = false
      sync()
    }
  }

  async function retry() {
    replaying.value = true
    try {
      await q().retry()
    } finally {
      replaying.value = false
      sync()
    }
  }

  /**
   * Check-in that survives a lost connection: on a network failure the action is
   * queued with its Idempotency-Key and replayed later. Other errors propagate.
   */
  async function checkin(input: CheckinInput, key: string): Promise<SubmitResult> {
    try {
      await api.postCheckin(input, key)
      return { queued: false }
    } catch (e) {
      if (e instanceof api.ApiError && e.status === 0) {
        await q().enqueue(input, key)
        sync()
        return { queued: true }
      }
      throw e
    }
  }

  function dismissFailure(key: string) {
    q().dismissFailure(key)
    sync()
  }

  /** Logout: nothing queued may outlive the session that created it. */
  async function clear() {
    await q().clear()
    sync()
    try {
      localStorage.removeItem(USER_KEY)
    } catch {
      // storage unavailable
    }
  }

  /** Account switch: queued actions belong to one user, so a different user id drops them. */
  async function bindUser(id: string) {
    let prev: string | null = null
    try {
      prev = localStorage.getItem(USER_KEY)
      localStorage.setItem(USER_KEY, id)
    } catch {
      // storage unavailable
    }
    if (prev !== null && prev !== id) await clear().then(() => bindUserStore(id))
  }

  function bindUserStore(id: string) {
    try {
      localStorage.setItem(USER_KEY, id)
    } catch {
      // storage unavailable
    }
  }

  let listening = false
  function listen() {
    if (listening || typeof window === 'undefined') return
    listening = true
    window.addEventListener('online', () => {
      online.value = true
      void replay()
    })
    window.addEventListener('offline', () => {
      online.value = false
    })
  }

  const visible = computed(() => !online.value || pendingCount.value > 0 || failures.value.length > 0)

  return {
    online, pendingCount, pendingItems, failures, paused, stalled, replaying, syncedTick, visible,
    start, replay, retry, checkin, dismissFailure, clear, bindUser, listen,
  }
})
