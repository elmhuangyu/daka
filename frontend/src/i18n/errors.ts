import type { MessageKey } from './en'

/**
 * Server error strings (the `error` field of an API error body) mapped to
 * message keys. The server never puts user data such as item names into these
 * strings, and neither do the translations. Anything not listed falls back to
 * a generic message chosen by HTTP status, so raw English never reaches the UI.
 */
export const ERROR_KEYS = {
  invalid_secret: 'errors.invalidSecret',
  too_many_attempts: 'errors.tooManyAttempts',
  unauthorized: 'errors.unauthorized',
  reauth_required: 'errors.reauthRequired',
  csrf: 'errors.csrf',
  cross_origin: 'errors.crossOrigin',
  login_failed: 'errors.loginFailed',
  'forbidden host': 'errors.forbiddenHost',
  'identity provider unavailable': 'errors.idpUnavailable',
  'internal error': 'errors.internal',
  'not found': 'errors.notFound',
  'item not found': 'errors.itemNotFound',
  'category not found': 'errors.categoryNotFound',
  'check-in not found': 'errors.checkinNotFound',
  'freeze not found': 'errors.freezeNotFound',
  'token not found': 'errors.tokenNotFound',
  'method not allowed': 'errors.methodNotAllowed',
  'invalid JSON body': 'errors.invalidBody',
  'invalid body': 'errors.invalidBody',
  'invalid request body': 'errors.invalidBody',
  'invalid id': 'errors.invalidId',
  'invalid item': 'errors.invalidId',
  'invalid category': 'errors.invalidCategory',
  'sub-tasks can nest at most 5 levels': 'errors.itemTooDeep',
  'the parent cannot be a descendant of the item': 'errors.invalidMove',
  'an item cannot be its own parent': 'errors.invalidMove',
  'use at most one of before_id and after_id': 'errors.invalidMove',
  'invalid anchor': 'errors.invalidMove',
  'anchor not found': 'errors.invalidMove',
  'anchor is not a sibling under the target parent': 'errors.invalidMove',
  'the item tree changed concurrently, retry': 'errors.treeConflict',
  'invalid move_to': 'errors.invalidCategory',
  'invalid mode': 'errors.invalidRequest',
  'invalid redact': 'errors.invalidRequest',
  'invalid year': 'errors.invalidRequest',
  'unknown source': 'errors.invalidRequest',
  'name is required': 'errors.nameRequired',
  'name too long': 'errors.nameTooLong',
  'note too long': 'errors.noteTooLong',
  'reason too long': 'errors.reasonTooLong',
  'description too long': 'errors.descriptionTooLong',
  'Idempotency-Key too long': 'errors.invalidRequest',
  'window too large': 'errors.rangeTooLarge',
  'range too large (max 93 days)': 'errors.rangeTooLarge',
  'invalid start or end': 'errors.invalidDate',
  'start must not be after end': 'errors.invalidDate',
  'start_date must not be after end_date': 'errors.invalidDate',
  'start_date and end_date must be YYYY-MM-DD': 'errors.invalidDate',
  'start and end must be YYYY-MM-DD': 'errors.invalidDate',
  'invalid date, expected YYYY-MM-DD': 'errors.invalidDate',
  'effective_from must be YYYY-MM-DD': 'errors.invalidEffectiveFrom',
  'remind_time must be HH:MM': 'errors.invalidRemindTime',
  'digest_time must be HH:MM or empty': 'errors.invalidDigestTime',
  'end too far in the future': 'errors.dateInFuture',
  'date is in the future': 'errors.dateInFuture',
  'freeze period too long (max 366 days)': 'errors.freezeTooLong',
  'tracker_type, kind and polarity cannot be changed': 'errors.immutableFields',
  'mode does not match item type': 'errors.modeMismatch',
  'item is archived': 'errors.itemArchived',
  "delta would make the day's total negative": 'errors.negativeTotal',
  "deleting this increment would make the day's total negative": 'errors.negativeTotal',
  'category has items; pass move_to or uncategorize=1': 'errors.categoryHasItems',
  'too many active tokens': 'errors.tooManyTokens',
  'api token not accepted here': 'errors.tokenNotAccepted',
  'telegram is not linked': 'errors.telegramNotLinked',
  'telegram is not configured': 'errors.telegramNotConfigured',
  'invalid bot token': 'errors.invalidBotToken',
  'bot token is required': 'errors.botTokenRequired',
  'chat already linked to another user': 'errors.telegramChatTaken',
  'telegram send failed': 'errors.telegramSendFailed',
  'encryption key not configured': 'errors.encryptionKeyMissing',
  'confirmation required': 'errors.confirmationRequired',
  'import file too large': 'errors.importTooLarge',
  'invalid import: not a valid daka export document': 'errors.importInvalid',
} as const satisfies Record<string, MessageKey>

export type ErrorCode = keyof typeof ERROR_KEYS

/** Machine codes of a failed external calendar fetch (`extcal.Code` on the server). */
export const EXT_CAL_ERROR_KEYS = {
  blocked_address: 'calendar.extErr.blocked_address',
  unsupported_scheme: 'calendar.extErr.unsupported_scheme',
  invalid_url: 'calendar.extErr.invalid_url',
  too_large: 'calendar.extErr.too_large',
  not_icalendar: 'calendar.extErr.not_icalendar',
  fetch_failed: 'calendar.extErr.fetch_failed',
} as const satisfies Record<string, MessageKey>

export function extCalErrorKey(code: string): MessageKey {
  return Object.prototype.hasOwnProperty.call(EXT_CAL_ERROR_KEYS, code)
    ? EXT_CAL_ERROR_KEYS[code as keyof typeof EXT_CAL_ERROR_KEYS]
    : 'calendar.extErr.unknown'
}

/** Prefix of import validation errors; the rest is a machine location such as `items[0]: bad_name`. */
export const IMPORT_ERROR_PREFIX = 'invalid import: '

/** Message key for a status-0 (network failure) error. */
export const NETWORK_ERROR_KEY = 'errors.network' satisfies MessageKey

/** Message key for a server error code, or null when the code is unknown. */
export function errorKeyForCode(code: string): MessageKey | null {
  return Object.prototype.hasOwnProperty.call(ERROR_KEYS, code)
    ? ERROR_KEYS[code as ErrorCode]
    : null
}

/** Generic fallback key by HTTP status, used when the code is empty or unknown. */
export function errorKeyForStatus(status: number): MessageKey {
  if (status === 0) return NETWORK_ERROR_KEY
  if (status === 401) return 'errors.unauthorized'
  if (status === 403) return 'errors.forbidden'
  if (status === 404) return 'errors.notFound'
  if (status === 429) return 'errors.tooManyAttempts'
  if (status >= 500) return 'errors.internal'
  return 'errors.generic'
}
