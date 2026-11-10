import { t } from './i18n'
import {
  IMPORT_ERROR_PREFIX,
  NETWORK_ERROR_KEY,
  errorKeyForCode,
  errorKeyForStatus,
} from './i18n/errors'

export interface TodayItem {
  id: number
  name: string
  category_id: number | null
  project_id?: number | null
  tracker_type: string
  kind?: string
  color?: string
  is_sensitive: boolean
  due: boolean
  done: boolean
  overdue?: boolean
  due_date?: string
  log_ids: number[]
}

export interface UpcomingItem {
  id: number
  name: string
  category_id: number | null
  project_id?: number | null
  tracker_type: string
  kind: string
  color?: string
  is_sensitive: boolean
  due_date: string
  days_until: number
}

export interface TodayView {
  date: string
  weekday: number // 0 = Sunday .. 6 = Saturday
  timezone: string
  progress: { done: number; total: number }
  items: TodayItem[]
  upcoming?: UpcomingItem[]
}

export class ApiError extends Error {
  status: number
  /** Machine-readable error code from the server body (`{"error": code}`), when present. */
  code: string
  /** Seconds from a Retry-After header (HTTP 429), when present. */
  retryAfter?: number
  /** Stable reason code of a 400 "invalid combination" answer, when present. */
  reason = ''
  constructor(message: string, status: number, code = '') {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

/**
 * Localized, user-facing text for an error. API errors map through the error
 * code table; an unknown code falls back to a generic message by status, so the
 * server's raw text is never shown. Other errors keep their own message.
 */
export function describeError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return t(NETWORK_ERROR_KEY)
    const key = errorKeyForCode(e.code)
    if (key) return t(key)
    // Import validation answers carry a language-independent location and code.
    if (e.code.startsWith(IMPORT_ERROR_PREFIX))
      return t('errors.importDetail', { detail: e.code.slice(IMPORT_ERROR_PREFIX.length) })
    return t(errorKeyForStatus(e.status), { status: e.status })
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

function getBroadcastChannelCtor(): typeof BroadcastChannel | undefined {
  return typeof BroadcastChannel !== 'undefined' ? BroadcastChannel : undefined
}

const AUTH_CHANNEL_NAME = 'daka_auth_channel'

export type AuthChannelMessage =
  | { type: 'logged_in' }
  | { type: 'logged_out' }
  | { type: 'session_expired' }

export function broadcastAuthEvent(msg: AuthChannelMessage): void {
  try {
    const Channel = getBroadcastChannelCtor()
    if (Channel) {
      const channel = new Channel(AUTH_CHANNEL_NAME)
      // postMessage queues the message on the channel. Per HTML BroadcastChannel
      // specification, close() only stops receiving new messages on this instance;
      // queued outgoing messages are dispatched to peer tabs.
      channel.postMessage(msg)
      channel.close()
    }
  } catch {
    /* BroadcastChannel not available or failed */
  }
}

export function onAuthBroadcast(handler: (msg: AuthChannelMessage) => void): () => void {
  try {
    const Channel = getBroadcastChannelCtor()
    if (Channel) {
      const channel = new Channel(AUTH_CHANNEL_NAME)
      channel.onmessage = (event: MessageEvent<AuthChannelMessage>) => {
        if (event.data && typeof event.data.type === 'string') {
          handler(event.data)
        }
      }
      return () => {
        try {
          channel.close()
        } catch {
          /* ignore close failure */
        }
      }
    }
  } catch {
    /* BroadcastChannel not available */
  }
  return () => {}
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
  avatar_url?: string
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
  broadcastAuthEvent({ type: 'logged_in' })
}

export async function logout(): Promise<void> {
  try {
    await request('/api/auth/logout', { method: 'POST' })
  } finally {
    resetCsrf()
    broadcastAuthEvent({ type: 'logged_out' })
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
  return request<ReauthResult>(
    '/api/auth/reauth',
    jsonInit('POST', secret === undefined ? {} : { secret }),
  )
}

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

interface Retry {
  csrf: boolean
  reauth: boolean
}

async function request<T>(
  path: string,
  init: RequestInit = {},
  retry: Retry = { csrf: false, reauth: false },
  as: 'json' | 'text' = 'json',
): Promise<T> {
  let res: Response
  const method = (init.method ?? 'GET').toUpperCase()
  // Started early for every request so writes do not wait for a cold start.
  const noToken =
    path === '/api/auth/me' || path === '/api/auth/login' || path === '/api/auth/status'
  const csrf = noToken ? Promise.resolve(null) : getCsrf()
  try {
    if (!SAFE_METHODS.has(method) && !noToken) {
      const token = await csrf
      if (token)
        init = {
          ...init,
          headers: {
            ...(init.headers as Record<string, string> | undefined),
            'X-Daka-CSRF': token,
          },
        }
    }
    res = await fetch(path, { credentials: 'same-origin', ...init })
  } catch {
    throw new ApiError('Network error', 0)
  }
  if (!res.ok) {
    let msg = `Request failed (${res.status})`
    let code = ''
    let reason = ''
    try {
      const body = (await res.json()) as { error?: string; reason?: string }
      if (body && typeof body.error === 'string') msg = code = body.error
      if (body && typeof body.reason === 'string') reason = body.reason
    } catch {
      // keep the default message
    }
    const err = new ApiError(msg, res.status, code)
    err.reason = reason
    const ra = Number(res.headers.get('Retry-After'))
    if (ra > 0) err.retryAfter = ra
    if (res.status === 401 || (res.status === 403 && code === 'csrf')) resetCsrf()
    if (res.status === 403 && code === 'csrf' && !retry.csrf && !noToken) {
      // The cached token went stale (new session); fetch a fresh one and retry once.
      return request<T>(path, stripCsrf(init), { ...retry, csrf: true }, as)
    }
    if (
      res.status === 403 &&
      code === 'reauth_required' &&
      !retry.reauth &&
      hooks.onReauthRequired
    ) {
      if (await hooks.onReauthRequired())
        return request<T>(path, init, { ...retry, reauth: true }, as)
    }
    if (res.status === 401 && code === 'unauthorized' && path !== '/api/auth/me') {
      broadcastAuthEvent({ type: 'session_expired' })
      hooks.onUnauthorized?.()
    }
    throw err
  }
  if (res.status === 204) return undefined as T
  if (as === 'text') return (await res.text()) as T
  return (await res.json()) as T
}

function stripCsrf(init: RequestInit): RequestInit {
  const h = { ...(init.headers as Record<string, string> | undefined) }
  delete h['X-Daka-CSRF']
  return { ...init, headers: h }
}

function jsonInit(
  method: string,
  body: unknown,
  headers: Record<string, string> = {},
): RequestInit {
  return {
    method,
    headers: { 'Content-Type': 'application/json', ...headers },
    body: JSON.stringify(body),
  }
}

export function getToday(redact: boolean, date?: string): Promise<TodayView> {
  const params = new URLSearchParams()
  if (redact) params.set('redact', '1')
  if (date) params.set('date', date)
  const qs = params.toString() ? `?${params.toString()}` : ''
  return request<TodayView>(`/api/view/today${qs}`)
}

export interface OptionDetail {
  id: number
  name: string
  unit: string
  step: number
  min_val: number | null
  max_val: number | null
  aggregate: string
  sort_order: number
  target_value: number | null
}

export interface RuleDetail {
  id?: number
  effective_from?: string
  schedule_type: 'fixed_rrule' | 'rolling' | 'quota' | 'none' | string
  rrule_string?: string
  dtstart?: string
  until?: string
  holiday_mode?: string
  rolling_interval?: number
  rolling_unit?: string
  target_value?: number
  completion_rule?: string
  option_targets: Record<string, number>
}

export interface QuotaProgress {
  comparator: string
  window: string
  period: string
  used: number
  limit: number
  remaining: number
  ratio: number
  met: boolean
  exceeded: boolean
  traffic?: 'green' | 'yellow' | 'red' | ''
}

export interface MaintenanceInfo {
  next_due: string
  state: 'ok' | 'warning' | 'overdue' | string
  days_until_due: number
  overdue_days: number
}

export interface ItemStatsSummary {
  last_at?: string | null
  days_since_last?: number | null
  counts?: { last_30_days: number; this_year: number; total: number }
  quota?: QuotaProgress | null
  maintenance?: MaintenanceInfo | null
}

/** One item of GET /api/items?include=stats (the fields the Today view uses). */
export interface ItemDetail {
  id: number
  name: string
  color?: string
  tracker_type: 'boolean' | 'counter' | 'maintenance' | 'metric' | string
  kind: 'habit' | 'event' | string
  polarity: 'positive' | 'negative' | string
  category_id: number | null
  project_id?: number | null
  effective_sensitive: boolean
  unit?: string
  step?: number
  min_val?: number | null
  max_val?: number | null
  aggregate?: string
  warning_days?: number
  pinned?: boolean
  archived?: boolean
  options: OptionDetail[]
  current_rule: RuleDetail
  stats?: ItemStatsSummary
}

export interface Category {
  id: number
  name: string
  color: string
  icon: string
  is_sensitive: boolean
  sort_order: number
}

export interface CheckinLog {
  id: number
  item_id: number
  option_id: number | null
  date: string
  value: number
  note: string
}

export interface CheckinInput {
  item_id: number
  mode: 'set' | 'unset' | 'delta' | 'append'
  option_id?: number
  value?: number
  note?: string
  /** One-off option (DESIGN 11.3): no option_id, the free-form name goes in note. */
  adhoc?: boolean
  date?: string
}

export function getItems(redact: boolean): Promise<{ items: ItemDetail[] }> {
  return request(`/api/items?include=stats${redact ? '&redact=1' : ''}`)
}

export function getCategories(): Promise<{ categories: Category[] }> {
  return request('/api/categories')
}

/** All check-in logs of one day (follows the cursor pagination). */
export async function getCheckinsOn(date: string, redact: boolean): Promise<CheckinLog[]> {
  const out: CheckinLog[] = []
  let cursor = ''
  for (let page = 0; page < 20; page++) {
    const q = `from=${date}&to=${date}&limit=200${redact ? '&redact=1' : ''}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`
    const res = await request<{ checkins?: CheckinLog[]; next_cursor?: string | null }>(
      `/api/checkins?${q}`,
    )
    out.push(...(res.checkins ?? []))
    if (!res.next_cursor) break
    cursor = res.next_cursor
  }
  return out
}

/**
 * POST /api/checkins. A key identifies one logical user action; callers pass
 * their own to make deliberate retries of that action idempotent.
 */
export function postCheckin(
  input: CheckinInput,
  key: string = newIdempotencyKey(),
): Promise<unknown> {
  return request('/api/checkins', jsonInit('POST', input, { 'Idempotency-Key': key }))
}

export function deleteCheckin(id: number): Promise<unknown> {
  return request(`/api/checkins/${id}`, {
    method: 'DELETE',
    headers: { 'Idempotency-Key': newIdempotencyKey() },
  })
}

export function patchCheckin(
  id: number,
  body: { note: string },
): Promise<{ id: number; item_id: number; date: string; note: string }> {
  return request(
    `/api/checkins/${id}`,
    jsonInit('PATCH', body, { 'Idempotency-Key': newIdempotencyKey() }),
  )
}

export function setCheckin(itemId: number, done: boolean): Promise<unknown> {
  return postCheckin({ item_id: itemId, mode: done ? 'set' : 'unset' })
}

// ---- Item editor (POST/PATCH /api/items, options, categories) ----

export interface ItemOptionDto {
  id: number
  name: string
  unit: string
  step: number
  min_val: number | null
  max_val: number | null
  aggregate: string
  sort_order: number
  target_value: number | null
}

export interface ItemRuleDto {
  schedule_type: string
  rrule_string: string
  dtstart: string
  until: string | null
  holiday_mode: string
  effective_from: string
  rolling_interval?: number
  rolling_unit?: string
  quota_comparator?: string
  quota_count?: number
  quota_period?: string
  quota_window?: string
  quota_measure?: string
  quota_option_id?: number
  target_value?: number
  completion_rule?: string
  completion_n?: number
  stale_after_days?: number
  option_targets: Record<string, number>
}

export interface ItemDto {
  id: number
  name: string
  description: string
  color?: string
  remind_time: string | null
  tracker_type: string
  kind: string
  polarity: string
  category_id: number | null
  project_id?: number | null
  is_sensitive: boolean
  effective_sensitive: boolean
  unit?: string
  step?: number
  min_val?: number | null
  max_val?: number | null
  aggregate?: string
  warning_days?: number
  pinned?: boolean
  archived?: boolean
  options: ItemOptionDto[]
  current_rule: ItemRuleDto
}

export type Item = ItemDto

export interface CategoryDto {
  id: number
  name: string
  color: string
  icon: string
  is_sensitive: boolean
}

export function getItem(id: number, redact = false): Promise<ItemDto> {
  return request<ItemDto>(`/api/items/${id}${redact ? '?redact=1' : ''}`)
}

export function postItem(body: unknown): Promise<ItemDto> {
  return request<ItemDto>('/api/items', jsonInit('POST', body))
}

export function patchItem(id: number, body: unknown): Promise<ItemDto> {
  return request<ItemDto>(`/api/items/${id}`, jsonInit('PATCH', body))
}

export function postOption(itemId: number, body: unknown): Promise<ItemOptionDto> {
  return request<ItemOptionDto>(`/api/items/${itemId}/options`, jsonInit('POST', body))
}

export function putOption(itemId: number, optionId: number, body: unknown): Promise<ItemOptionDto> {
  return request<ItemOptionDto>(`/api/items/${itemId}/options/${optionId}`, jsonInit('PUT', body))
}

export function deleteOption(itemId: number, optionId: number): Promise<unknown> {
  return request(`/api/items/${itemId}/options/${optionId}`, { method: 'DELETE' })
}

export async function listCategories(): Promise<CategoryDto[]> {
  return (await request<{ categories: CategoryDto[] }>('/api/categories')).categories
}

export function createCategory(name: string, isSensitive: boolean): Promise<CategoryDto> {
  return request<CategoryDto>(
    '/api/categories',
    jsonInit('POST', { name, is_sensitive: isSensitive }),
  )
}

// ---- Calendar view (GET /api/view/calendar, /api/settings/calendar, /api/calendar/external) ----

export type CalItemStatus =
  | 'done'
  | 'partial'
  | 'missed'
  | 'frozen'
  | 'pending'
  | 'upcoming'
  | 'extra'
export type CalDayStatus =
  | 'none'
  | 'complete'
  | 'partial'
  | 'incomplete'
  | 'pending'
  | 'upcoming'
  | 'frozen'

export interface CalendarDayItem {
  item_id: number
  name: string
  category_id: number | null
  project_id?: number | null
  tracker_type: string
  is_sensitive: boolean
  due: boolean
  status: CalItemStatus
  done: boolean
  log_count: number
  has_supplement: boolean
  forecast: boolean
  overdue: boolean
}

export interface CalendarDay {
  date: string
  weekday: number
  day_type: 'normal' | 'holiday' | 'workday' | string
  holiday_name?: string
  is_off_day: boolean
  makeup_workday: boolean
  frozen: boolean
  status: CalDayStatus
  due_count: number
  done_count: number
  partial_count: number
  has_overdue: boolean
  items: CalendarDayItem[]
}

export interface CalendarViewData {
  start: string
  end: string
  timezone: string
  today: string
  week_start: number
  holiday_region: string
  holiday_published: boolean
  freezes: { id: number; start_date: string; end_date: string; reason: string }[]
  days: CalendarDay[]
}

export function getCalendarView(
  start: string,
  end: string,
  redact: boolean,
): Promise<CalendarViewData> {
  return request(`/api/view/calendar?start=${start}&end=${end}${redact ? '&redact=1' : ''}`)
}

export interface ExternalEvent {
  title: string
  /** YYYY-MM-DD for all-day events (end inclusive), RFC 3339 otherwise. */
  start: string
  end: string
  all_day?: boolean
}

export interface ExternalSource {
  id: string
  name: string
  ok: boolean
  error?: string
  events: ExternalEvent[]
  truncated?: boolean
}

export function getExternalCalendar(
  start: string,
  end: string,
  sourceId: string,
): Promise<{ sources: ExternalSource[] }> {
  return request(
    `/api/calendar/external?start=${start}&end=${end}&source_id=${encodeURIComponent(sourceId)}`,
  )
}

export function createAdHocEvent(name: string): Promise<{ id: number }> {
  return request<{ id: number }>(
    '/api/items',
    jsonInit('POST', {
      name: name.trim(),
      tracker_type: 'boolean',
      kind: 'event',
      polarity: 'positive',
      rule: { schedule_type: 'none', holiday_mode: 'none' },
    }),
  )
}

export function createScheduledEvent(name: string, date: string): Promise<{ id: number }> {
  return request<{ id: number }>(
    '/api/items',
    jsonInit('POST', {
      name: name.trim(),
      tracker_type: 'boolean',
      kind: 'event',
      polarity: 'positive',
      rule: {
        schedule_type: 'fixed_rrule',
        rrule_string: 'FREQ=DAILY;COUNT=1',
        dtstart: date,
        until: date,
        holiday_mode: 'none',
      },
    }),
  )
}

export function createTodoItem(name: string, dueDate?: string | null): Promise<{ id: number }> {
  if (dueDate && dueDate.trim()) {
    return createScheduledEvent(name, dueDate.trim())
  }
  return createAdHocEvent(name)
}

export function setTodoDueDate(itemId: number, dueDate: string | null): Promise<ItemDto> {
  if (dueDate && dueDate.trim()) {
    return patchItem(itemId, {
      rule: {
        schedule_type: 'fixed_rrule',
        rrule_string: 'FREQ=DAILY;COUNT=1',
        dtstart: dueDate.trim(),
        until: dueDate.trim(),
        holiday_mode: 'none',
      },
    })
  }
  return patchItem(itemId, {
    rule: {
      schedule_type: 'none',
      holiday_mode: 'none',
    },
  })
}

// ---- Settings (P3-5) ----

export interface ExternalCalendarDto {
  id: string
  name: string
  url_hint: string
}

export interface CalendarSettings {
  timezone: string
  week_start: number
  holiday_region: string
  streak_holiday_freeze: boolean
  external_calendars: ExternalCalendarDto[]
}

export function getCalendarSettings(): Promise<CalendarSettings> {
  return request<CalendarSettings>('/api/settings/calendar')
}

/** Partial update; `external_calendars` entries without `url` keep their stored URL. */
export function putCalendarSettings(body: {
  timezone?: string
  week_start?: number
  holiday_region?: string
  streak_holiday_freeze?: boolean
  external_calendars?: { id?: string; name: string; url?: string }[]
}): Promise<CalendarSettings> {
  return request<CalendarSettings>('/api/settings/calendar', jsonInit('PUT', body))
}

export interface ExternalSourceStatus {
  id: string
  name: string
  ok: boolean
  error?: string
}

export async function getExternalStatus(day: string): Promise<ExternalSourceStatus[]> {
  const r = await request<{ sources: ExternalSourceStatus[] }>(
    `/api/calendar/external?start=${day}&end=${day}`,
  )
  return r.sources
}

export interface TokenDto {
  id: number
  name: string
  scopes: string[]
  privacy: string
  include_negative: boolean
  category_id?: number | null
  expires_at: string | null
  expired: boolean
  revoked_at: string | null
  last_used_at: string | null
  /** Creation response only. */
  token?: string
  feed_path?: string
}

export async function listTokens(): Promise<TokenDto[]> {
  return (await request<{ tokens?: TokenDto[] }>('/api/tokens')).tokens ?? []
}

export function createToken(body: {
  name: string
  privacy?: string
  category_id?: number | null
  expires_in_days?: number
  never_expires?: boolean
  include_negative?: boolean
}): Promise<TokenDto> {
  return request<TokenDto>('/api/tokens', jsonInit('POST', body))
}

export function revokeToken(id: number): Promise<unknown> {
  return request(`/api/tokens/${id}`, { method: 'DELETE' })
}

export function deleteToken(id: number): Promise<unknown> {
  return request(`/api/tokens/${id}?delete=1`, { method: 'DELETE' })
}

export interface TelegramStatus {
  configured: boolean
  has_token?: boolean
  linked: boolean
  enabled: boolean
  remind_sensitive: boolean
  digest_time: string
  linked_at: string | null
  bot_username?: string
}

export function getTelegram(): Promise<TelegramStatus> {
  return request<TelegramStatus>('/api/telegram')
}

export function setupTelegramBot(body: {
  bot_token?: string
  chat_id?: number
}): Promise<TelegramStatus> {
  return request<TelegramStatus>('/api/telegram', jsonInit('PUT', body))
}

export function testTelegram(): Promise<{ sent?: boolean; ok?: boolean }> {
  return request<{ sent?: boolean; ok?: boolean }>('/api/telegram/test', { method: 'POST' })
}

export function createTelegramLinkCode(): Promise<{
  code: string
  expires_at: string
  deep_link?: string
}> {
  return request('/api/telegram/link-code', { method: 'POST' })
}

export function unlinkTelegram(): Promise<unknown> {
  return request('/api/telegram', { method: 'DELETE' })
}

export function patchTelegram(body: {
  enabled?: boolean
  remind_sensitive?: boolean
  digest_time?: string
}): Promise<TelegramStatus> {
  return request<TelegramStatus>('/api/telegram/settings', jsonInit('PATCH', body))
}

export interface CategoryChange {
  id: number
  name: string
  is_sensitive: boolean
  affected_items?: number
}

/** `confirmExposure` leaves items of a category that stops being sensitive exposed instead of freezing them. */
export function updateCategory(
  id: number,
  body: { name?: string; is_sensitive?: boolean },
  confirmExposure = false,
): Promise<CategoryChange> {
  return request<CategoryChange>(
    `/api/categories/${id}${confirmExposure ? '?confirm_exposure=1' : ''}`,
    jsonInit('PUT', body),
  )
}

export function removeCategory(
  id: number,
  how: { moveTo: number } | { uncategorize: true },
  confirmExposure = false,
): Promise<{ deleted: boolean; moved_items: number; affected_items: number }> {
  const q = new URLSearchParams()
  if ('moveTo' in how) q.set('move_to', String(how.moveTo))
  else q.set('uncategorize', '1')
  if (confirmExposure) q.set('confirm_exposure', '1')
  return request(`/api/categories/${id}?${q}`, { method: 'DELETE' })
}

// ---- Projects ----

export type ProjectStatus = 'active' | 'completed' | 'archived'

export interface Project {
  id: number
  category_id: number | null
  name: string
  description: string
  color: string
  status: ProjectStatus
  target_date: string | null
  is_sensitive: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface CreateProjectRequest {
  name: string
  description?: string
  color?: string
  status?: ProjectStatus
  target_date?: string | null
  category_id?: number | null
  is_sensitive?: boolean
  sort_order?: number
}

export interface UpdateProjectRequest {
  name?: string
  description?: string
  color?: string
  status?: ProjectStatus
  target_date?: string | null
  category_id?: number | null
  is_sensitive?: boolean
  sort_order?: number
}

export function listProjects(params?: {
  status?: ProjectStatus
  category_id?: number
  redact?: boolean
}): Promise<Project[]> {
  const q = new URLSearchParams()
  if (params?.status) q.set('status', params.status)
  if (params?.category_id !== undefined && params?.category_id !== null) {
    q.set('category_id', String(params.category_id))
  }
  if (params?.redact) q.set('redact', '1')
  const qs = q.toString()
  return request<Project[]>(`/api/projects${qs ? `?${qs}` : ''}`)
}

export function getProject(id: number, redact = false): Promise<Project> {
  return request<Project>(`/api/projects/${id}${redact ? '?redact=1' : ''}`)
}

export function createProject(data: CreateProjectRequest): Promise<Project> {
  return request<Project>('/api/projects', jsonInit('POST', data))
}

export function updateProject(
  id: number,
  data: UpdateProjectRequest,
  confirmExposure = false,
): Promise<Project> {
  return request<Project>(
    `/api/projects/${id}${confirmExposure ? '?confirm_exposure=1' : ''}`,
    jsonInit('PATCH', data),
  )
}

export function deleteProject(
  id: number,
  options?: { cascade?: boolean; unlink?: boolean; confirmExposure?: boolean },
): Promise<{ deleted: boolean }> {
  const q = new URLSearchParams()
  if (options?.cascade) q.set('cascade', '1')
  if (options?.unlink) q.set('unlink', '1')
  if (options?.confirmExposure) q.set('confirm_exposure', '1')
  const qs = q.toString()
  return request<{ deleted: boolean }>(`/api/projects/${id}${qs ? `?${qs}` : ''}`, {
    method: 'DELETE',
  })
}

// ---- Items panel and detail page ----

/** GET /api/items with stats for the items panel (search is skipped by callers while redacting). */
export function listItemsPanel(opts: {
  q?: string
  archived?: boolean
  redact: boolean
}): Promise<{ items: ItemDetail[] }> {
  const p = new URLSearchParams({ include: 'stats', kind: 'habit' })
  if (opts.q) p.set('q', opts.q)
  if (opts.archived) p.set('archived', 'true')
  if (opts.redact) p.set('redact', '1')
  return request(`/api/items?${p.toString()}`)
}

export interface HeatmapDay {
  date: string
  level: number
  value: number
  /** Completed / total options for the day; 0/0 for negative items and empty days. */
  options_met?: number
  options_total?: number
}

export interface OptionSummaryDto {
  option_id: number | null
  name: string
  unit: string
  count: number
  sum: number
  avg: number
  last_value: number
  last_date?: string
}

export interface ItemStats {
  item_id: number
  name: string
  today: string
  last_at: string | null
  last_date: string | null
  days_since_last: number | null
  summary: string
  counts: { last_30_days: number; this_year: number; total: number }
  active_days: { last_30_days: number; this_year: number; total: number }
  options: OptionSummaryDto[]
  streak: { applicable: boolean; unit?: string; current: number; longest: number }
  completion_rate_90: number | null
  heatmap: HeatmapDay[]
  quota: QuotaProgress | null
  maintenance: MaintenanceInfo | null
}

export function getItemStats(id: number, redact: boolean): Promise<ItemStats> {
  return request(`/api/items/${id}/stats${redact ? '?redact=1' : ''}`)
}

export interface HistoryLog {
  id: number
  item_id: number
  option_id: number | null
  date: string
  time: string
  value: number
  note: string
  source: string
  is_supplement: boolean
}

export function getItemCheckins(
  itemId: number,
  cursor: string,
  redact: boolean,
  limit = 20,
): Promise<{ checkins?: HistoryLog[]; next_cursor?: string | null }> {
  const p = new URLSearchParams({ item_id: String(itemId), limit: String(limit) })
  if (cursor) p.set('cursor', cursor)
  if (redact) p.set('redact', '1')
  return request(`/api/checkins?${p.toString()}`)
}

export interface ItemCalendarDay {
  date: string
  weekday: number
  status: string
  frozen: boolean
  items: Array<{
    item_id: number
    due: boolean
    status: string
    done: boolean
    forecast: boolean
    overdue: boolean
    log_count: number
  }>
}

export function getItemCalendar(
  itemId: number,
  start: string,
  end: string,
  redact: boolean,
): Promise<{ today: string; week_start: number; days: ItemCalendarDay[] }> {
  const p = new URLSearchParams({ start, end, item: String(itemId) })
  if (redact) p.set('redact', '1')
  return request(`/api/view/calendar?${p.toString()}`)
}

/** DELETE /api/items/{id}; the 403 reauth_required hook retries it after confirmation. */
export function deleteItem(id: number): Promise<unknown> {
  return request(`/api/items/${id}`, { method: 'DELETE' })
}

// --- Data: export / import / delete account (all behind reauth) ---

/** GET /api/export: the raw JSON document text (kept as text so it is saved byte for byte). */
export function exportData(): Promise<string> {
  return request<string>('/api/export', {}, { csrf: false, reauth: false }, 'text')
}

export interface ImportSummary {
  dry_run: boolean
  categories: number
  items: number
  options: number
  rule_versions: number
  checkins: number
  freezes: number
  settings_applied: boolean
}

/** POST /api/import with the text of an export file; dryRun only validates and counts. */
export function importData(
  text: string,
  opts: { dryRun?: boolean; applySettings?: boolean } = {},
): Promise<ImportSummary> {
  const q = new URLSearchParams()
  if (opts.dryRun) q.set('dry_run', '1')
  if (opts.applySettings) q.set('apply_settings', '1')
  const qs = q.toString()
  return request<ImportSummary>(`/api/import${qs ? `?${qs}` : ''}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: text,
  })
}

/** DELETE /api/account: permanently deletes the signed-in user and all their data. */
export function deleteAccount(): Promise<unknown> {
  return request('/api/account', jsonInit('DELETE', { confirm: 'DELETE' }))
}
