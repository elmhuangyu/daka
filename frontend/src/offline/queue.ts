import { ApiError, describeError, type CheckinInput } from '../api'
import { t } from '../i18n'

/**
 * Offline check-in queue (DESIGN 7.3). An entry holds only what the POST
 * /api/checkins request needs: ids, mode, value, date and note. Item names
 * are never stored. Ad-hoc check-ins are refused here (their note is the
 * option name, i.e. plaintext), and the offline store also refuses notes for
 * sensitive or unknown items, so IndexedDB never holds sensitive free text. Each entry keeps the Idempotency-Key of the original user
 * action, so a replay after a lost response cannot double-apply it.
 */
export interface QueuedCheckin {
  /** Auto-increment id; also the replay order. */
  seq?: number
  key: string
  input: Pick<CheckinInput, 'item_id' | 'mode' | 'value' | 'date' | 'note' | 'option_id' | 'adhoc'>
  createdAt: number
  attempts: number
}

export interface QueueStore {
  add(e: Omit<QueuedCheckin, 'seq'>): Promise<number>
  all(): Promise<QueuedCheckin[]>
  remove(seq: number): Promise<void>
  update(e: QueuedCheckin): Promise<void>
  clear(): Promise<void>
}

export interface QueueFailure {
  key: string
  item_id: number
  message: string
}

export type PauseReason = '' | 'unauthorized' | 'reauth_required'

export type Sender = (input: CheckinInput, key: string) => Promise<unknown>

/** YYYY-MM-DD of now in an IANA timezone (browser-local when absent or invalid). */
export function dateIn(tz?: string, now: Date = new Date()): string {
  try {
    return new Intl.DateTimeFormat('en-CA', {
      timeZone: tz || undefined,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(now)
  } catch {
    return new Intl.DateTimeFormat('en-CA', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).format(now)
  }
}

export function memoryStore(): QueueStore {
  let rows: QueuedCheckin[] = []
  let next = 1
  return {
    add: async (e) => {
      const seq = next++
      rows.push({ ...e, seq })
      return seq
    },
    all: async () => rows.map((r) => ({ ...r })),
    remove: async (seq) => {
      rows = rows.filter((r) => r.seq !== seq)
    },
    update: async (e) => {
      rows = rows.map((r) => (r.seq === e.seq ? { ...e } : r))
    },
    clear: async () => {
      rows = []
    },
  }
}

const DB_NAME = 'daka-offline'
const STORE = 'checkins'

function reqP<T>(r: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result)
    r.onerror = () => reject(r.error)
  })
}

/** IndexedDB-backed store; falls back to memory when IndexedDB is unavailable. */
export function idbStore(): QueueStore {
  if (typeof indexedDB === 'undefined') return memoryStore()
  let dbp: Promise<IDBDatabase> | null = null
  const db = () =>
    (dbp ??= new Promise<IDBDatabase>((resolve, reject) => {
      const open = indexedDB.open(DB_NAME, 1)
      open.onupgradeneeded = () =>
        open.result.createObjectStore(STORE, { keyPath: 'seq', autoIncrement: true })
      open.onsuccess = () => resolve(open.result)
      open.onerror = () => reject(open.error)
    }))
  const os = async (mode: IDBTransactionMode) =>
    (await db()).transaction(STORE, mode).objectStore(STORE)
  return {
    add: async (e) => Number(await reqP((await os('readwrite')).add(e))),
    all: async () =>
      ((await reqP((await os('readonly')).getAll())) as QueuedCheckin[]).sort(
        (a, b) => a.seq! - b.seq!,
      ),
    remove: async (seq) => void (await reqP((await os('readwrite')).delete(seq))),
    update: async (e) => void (await reqP((await os('readwrite')).put(e))),
    clear: async () => void (await reqP((await os('readwrite')).clear())),
  }
}

function pickInput(i: CheckinInput): QueuedCheckin['input'] {
  const out: QueuedCheckin['input'] = { item_id: i.item_id, mode: i.mode }
  if (i.value !== undefined) out.value = i.value
  if (i.date !== undefined) out.date = i.date
  if (i.note !== undefined) out.note = i.note
  if (i.option_id !== undefined) out.option_id = i.option_id
  if (i.adhoc) out.adhoc = true
  return out
}

/** Definitive client errors are dropped; 408/429 and 5xx are transient. */
function isDefinitive(status: number): boolean {
  return status >= 400 && status < 500 && status !== 408 && status !== 429
}

export class OfflineQueue {
  private running: Promise<void> | null = null
  private timer: ReturnType<typeof setTimeout> | null = null
  private generation = 0
  pending: QueuedCheckin[] = []
  failures: QueueFailure[] = []
  paused: PauseReason = ''
  /** Network or server trouble stopped the last replay (a retry is scheduled). */
  stalled = false
  onChange: () => void = () => {}

  constructor(
    private store: QueueStore,
    private send: Sender,
    private beforeReplay: () => void = () => {},
    private schedule: (fn: () => void, ms: number) => ReturnType<typeof setTimeout> = (fn, ms) =>
      setTimeout(fn, ms),
  ) {}

  async load(): Promise<void> {
    this.pending = await this.store.all()
    this.onChange()
  }

  async enqueue(input: CheckinInput, key: string): Promise<void> {
    if (input.adhoc) throw new Error(t('offline.adhocNeedsConnection'))
    const e: Omit<QueuedCheckin, 'seq'> = {
      key,
      input: pickInput(input),
      createdAt: Date.now(),
      attempts: 0,
    }
    const seq = await this.store.add(e)
    this.pending = [...this.pending, { ...e, seq }]
    this.onChange()
  }

  /** Schedules a replay attempt (used when the browser claims to be online but the request failed). */
  scheduleRetry(ms = 2000): void {
    if (this.timer || this.paused) return
    this.timer = this.schedule(() => void this.replay(), ms)
  }

  /** Item ids with a queued, not yet confirmed action. */
  pendingItemIds(): Set<number> {
    return new Set(this.pending.map((p) => p.input.item_id))
  }

  dismissFailure(key: string): void {
    this.failures = this.failures.filter((f) => f.key !== key)
    this.onChange()
  }

  /** Drops everything (logout / account switch). Also cancels pending retries and any running replay's effects. */
  async clear(): Promise<void> {
    this.generation++
    if (this.timer) clearTimeout(this.timer)
    this.timer = null
    this.pending = []
    this.failures = []
    this.paused = ''
    this.stalled = false
    await this.store.clear()
    this.onChange()
  }

  /** Replays queued actions in order. Concurrent calls share one run. */
  replay(): Promise<void> {
    this.running ??= this.run().finally(() => {
      this.running = null
    })
    return this.running
  }

  /** Manual retry: lifts a pause and replays now. */
  retry(): Promise<void> {
    this.paused = ''
    return this.replay()
  }

  private async run(): Promise<void> {
    if (this.timer) clearTimeout(this.timer)
    this.timer = null
    const gen = this.generation
    this.pending = await this.store.all()
    this.stalled = false
    if (this.paused) return this.onChange()
    for (const e of [...this.pending]) {
      if (gen !== this.generation) return
      this.beforeReplay() // fresh CSRF token for every replay
      try {
        await this.send({ ...e.input }, e.key)
      } catch (err) {
        if (gen !== this.generation) return
        const status = err instanceof ApiError ? err.status : 0
        const code = err instanceof ApiError ? err.code : ''
        if (status === 401) {
          this.paused = 'unauthorized'
        } else if (status === 403 && code === 'reauth_required') {
          this.paused = 'reauth_required'
        } else if (err instanceof ApiError && isDefinitive(status)) {
          await this.store.remove(e.seq!)
          this.pending = this.pending.filter((p) => p.seq !== e.seq)
          this.failures = [
            ...this.failures,
            { key: e.key, item_id: e.input.item_id, message: describeError(err) },
          ]
          continue
        } else {
          // Offline or transient: keep order, back off and try again.
          e.attempts++
          await this.store.update(e)
          this.stalled = true
          this.timer = this.schedule(
            () => void this.replay(),
            Math.min(60_000, 1000 * 2 ** Math.min(e.attempts, 6)),
          )
        }
        this.onChange()
        return
      }
      if (gen !== this.generation) return
      await this.store.remove(e.seq!)
      this.pending = this.pending.filter((p) => p.seq !== e.seq)
      this.onChange()
    }
    this.onChange()
  }
}
