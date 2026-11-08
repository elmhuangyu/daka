export interface TodayItem {
  id: number
  name: string
  category_id: number | null
  tracker_type: string
  is_sensitive: boolean
  due: boolean
  done: boolean
  log_ids: number[]
}

export interface TodayView {
  date: string
  weekday: string
  timezone: string
  progress: { done: number; total: number }
  items: TodayItem[]
}

export interface NewItem {
  name: string
  rrule_string: string
}

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

export function newIdempotencyKey(): string {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  // crypto.randomUUID is unavailable in insecure contexts (plain http); build a v4 UUID.
  const b = crypto.getRandomValues(new Uint8Array(16))
  b[6] = (b[6] & 0x0f) | 0x40
  b[8] = (b[8] & 0x3f) | 0x80
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    throw new ApiError('Network error', 0)
  }
  if (!res.ok) {
    let msg = `Request failed (${res.status})`
    try {
      const body = (await res.json()) as { error?: string }
      if (body && typeof body.error === 'string') msg = body.error
    } catch {
      // keep the default message
    }
    throw new ApiError(msg, res.status)
  }
  return (await res.json()) as T
}

function jsonInit(method: string, body: unknown, headers: Record<string, string> = {}): RequestInit {
  return { method, headers: { 'Content-Type': 'application/json', ...headers }, body: JSON.stringify(body) }
}

export function getToday(redact: boolean): Promise<TodayView> {
  return request<TodayView>(`/api/view/today${redact ? '?redact=1' : ''}`)
}

// Note: the idempotency key is generated per call; it should become per logical user action when counter delta arrives.
export function setCheckin(itemId: number, done: boolean): Promise<unknown> {
  return request(
    '/api/checkins',
    jsonInit('POST', { item_id: itemId, mode: done ? 'set' : 'unset' }, { 'Idempotency-Key': newIdempotencyKey() }),
  )
}

export function createItem(item: NewItem): Promise<unknown> {
  return request(
    '/api/items',
    jsonInit('POST', {
      name: item.name,
      tracker_type: 'boolean',
      kind: 'habit',
      polarity: 'positive',
      rule: { schedule_type: 'fixed_rrule', rrule_string: item.rrule_string, holiday_mode: 'none' },
    }),
  )
}
