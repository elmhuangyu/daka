import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import * as api from '../api'
import { t } from '../i18n'
import type { CheckinInput } from '../api'
import { useAuthStore } from './auth'
import { useCalendarStore } from './calendar'
import { useTodayStore } from './today'
import {
  dateIn,
  idbStore,
  OfflineQueue,
  type PauseReason,
  type QueueFailure,
} from '../offline/queue'

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
  /** Identity bound (queue cleared on account mismatch); nothing replays before this. */
  const bound = ref(false)

  function q(): OfflineQueue {
    if (!queue) {
      queue = new OfflineQueue(
        idbStore(),
        (i, k) => api.postCheckin(i, k),
        () => api.resetCsrf(),
      )
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
    // Wait for auth status and the user binding (which clears a foreign queue) first.
    await useAuthStore().init()
    await q().load()
    sync()
    if (pendingCount.value > 0) void replay()
  }

  async function replay() {
    if (!bound.value) return // unknown identity: never replay
    replaying.value = true
    try {
      await q().replay()
    } finally {
      replaying.value = false
      sync()
    }
  }

  async function retry() {
    if (!bound.value) return
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
        if (input.adhoc || (input.note && isPrivateOrUnknown(input.item_id))) {
          throw new Error(t('offline.needsConnection'))
        }
        // Stamp the day now: a replay after midnight must not land on the wrong day.
        const tz = useTodayStore().view?.timezone
        await q().enqueue(input.date ? input : { ...input, date: dateIn(tz) }, key)
        if (typeof navigator !== 'undefined' && navigator.onLine) q().scheduleRetry()
        sync()
        return { queued: true }
      }
      throw e
    }
  }

  /** True unless the item is known to be non-sensitive (privacy mode drops item data, so unknown counts as private). */
  function isPrivateOrUnknown(itemId: number): boolean {
    const d = [...useTodayStore().items, ...useCalendarStore().items].find((i) => i.id === itemId)
    return !d || d.effective_sensitive
  }

  function dismissFailure(key: string) {
    q().dismissFailure(key)
    sync()
  }

  /** Logout: nothing queued may outlive the session that created it. */
  async function clear() {
    bound.value = false
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
    bound.value = true
    await q().load()
    sync()
    if (pendingCount.value > 0) void replay()
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

  const visible = computed(
    () => !online.value || pendingCount.value > 0 || failures.value.length > 0,
  )

  return {
    online,
    pendingCount,
    pendingItems,
    failures,
    paused,
    stalled,
    replaying,
    syncedTick,
    visible,
    bound,
    start,
    replay,
    retry,
    checkin,
    dismissFailure,
    clear,
    bindUser,
    listen,
  }
})
