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
  weekday: number // 0 = Sunday .. 6 = Saturday
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
  /** Machine-readable error code from the server body (`{"error": code}`), when present. */
  code: string
  /** Seconds from a Retry-After header (HTTP 429), when present. */
  retryAfter?: number
  constructor(message: string, status: number, code = '') {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

const FRIENDLY: Record<string, string> = {
  invalid_secret: 'Invalid secret',
  too_many_attempts: 'Too many attempts, try again later',
  unauthorized: 'You are signed out',
  reauth_required: 'Confirm your identity to continue',
  csrf: 'Session check failed, please retry',
  cross_origin: 'Cross-origin request rejected',
  login_failed: 'Login failed',
}

/** Human-readable English text for an error; unknown server messages pass through. */
export function describeError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return 'Network error'
    return FRIENDLY[e.code] ?? e.message
  }
  return e instanceof Error ? e.message : String(e)
}

/**
 * Hooks installed by the auth store. They keep this module free of UI and
 * store imports.
 */
export interface AuthHooks {
  /** Called when the server says the session is gone (401 unauthorized). */
  onUnauthorized?: () => void
  /** Called on 403 reauth_required; resolve true once reauth succeeded to retry the request. */
  onReauthRequired?: () => Promise<boolean>
}

let hooks: AuthHooks = {}

export function setAuthHooks(h: AuthHooks): void {
  hooks = h
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

export interface Me {
  id: string
  name: string
  email?: string
  csrf_token?: string
}

// The CSRF token is session-bound and served by GET /api/auth/me. It is only
// present when a cookie session exists; in loopback single-user mode there is
// none and no header is sent (the server then relies on same-origin checks).
let csrfPromise: Promise<string | null> | null = null

async function loadCsrf(): Promise<string | null> {
  try {
    const res = await fetch('/api/auth/me')
    if (!res.ok) return null
    const me = (await res.json()) as Partial<Me>
    return typeof me?.csrf_token === 'string' && me.csrf_token ? me.csrf_token : null
  } catch {
    return null
  }
}

function getCsrf(): Promise<string | null> {
  if (!csrfPromise) {
    const p = loadCsrf()
    csrfPromise = p
    // Do not cache a miss (e.g. not logged in yet): retry on the next write.
    void p.then((t) => {
      if (t === null && csrfPromise === p) csrfPromise = null
    })
  }
  return csrfPromise
}

/** Forget the cached CSRF token (after login / logout or a CSRF rejection; also for tests). */
export function resetCsrf(): void {
  csrfPromise = null
}

export function getMe(): Promise<Me> {
  return request<Me>('/api/auth/me')
}

export interface AuthStatus {
  oidc_enabled: boolean
  login_required: boolean
  authenticated: boolean
}

export function getAuthStatus(): Promise<AuthStatus> {
  return request<AuthStatus>('/api/auth/status')
}

/** Single-user shared-secret login (POST /api/auth/login). */
export async function login(secret: string): Promise<void> {
  await request('/api/auth/login', jsonInit('POST', { secret }))
  resetCsrf()
}

export async function logout(): Promise<void> {
  try {
    await request('/api/auth/logout', { method: 'POST' })
  } finally {
    resetCsrf()
  }
}

/** OIDC login entry point; navigate the browser here (full page redirect). */
export const OIDC_LOGIN_URL = '/api/auth/oidc/login'

export interface ReauthResult {
  ok?: boolean
  /** OIDC mode: navigate here to re-authenticate at the identity provider. */
  redirect?: string
}

/** POST /api/auth/reauth: shared secret in secret mode, redirect target in OIDC mode. */
export function reauth(secret?: string): Promise<ReauthResult> {
  return request<ReauthResult>('/api/auth/reauth', jsonInit('POST', secret === undefined ? {} : { secret }))
}

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

interface Retry {
  csrf: boolean
  reauth: boolean
}

async function request<T>(path: string, init: RequestInit = {}, retry: Retry = { csrf: false, reauth: false }): Promise<T> {
  let res: Response
  const method = (init.method ?? 'GET').toUpperCase()
  // Started early for every request so writes do not wait for a cold start.
  const noToken = path === '/api/auth/me' || path === '/api/auth/login' || path === '/api/auth/status'
  const csrf = noToken ? Promise.resolve(null) : getCsrf()
  try {
    if (!SAFE_METHODS.has(method) && !noToken) {
      const token = await csrf
      if (token) init = { ...init, headers: { ...(init.headers as Record<string, string> | undefined), 'X-Daka-CSRF': token } }
    }
    res = await fetch(path, { credentials: 'same-origin', ...init })
  } catch {
    throw new ApiError('Network error', 0)
  }
  if (!res.ok) {
    let msg = `Request failed (${res.status})`
    let code = ''
    try {
      const body = (await res.json()) as { error?: string }
      if (body && typeof body.error === 'string') msg = code = body.error
    } catch {
      // keep the default message
    }
    const err = new ApiError(msg, res.status, code)
    const ra = Number(res.headers.get('Retry-After'))
    if (ra > 0) err.retryAfter = ra
    if (res.status === 401 || (res.status === 403 && code === 'csrf')) resetCsrf()
    if (res.status === 403 && code === 'csrf' && !retry.csrf && !noToken) {
      // The cached token went stale (new session); fetch a fresh one and retry once.
      return request<T>(path, stripCsrf(init), { ...retry, csrf: true })
    }
    if (res.status === 403 && code === 'reauth_required' && !retry.reauth && hooks.onReauthRequired) {
      if (await hooks.onReauthRequired()) return request<T>(path, init, { ...retry, reauth: true })
    }
    if (res.status === 401 && code === 'unauthorized' && path !== '/api/auth/me') hooks.onUnauthorized?.()
    throw err
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

function stripCsrf(init: RequestInit): RequestInit {
  const h = { ...(init.headers as Record<string, string> | undefined) }
  delete h['X-Daka-CSRF']
  return { ...init, headers: h }
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
