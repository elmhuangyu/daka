import { computed, ref } from 'vue'
import { en, type MessageKey } from './en'
import {
  DEFAULT_LOCALE,
  LOCALE_STORAGE_KEY,
  isSupportedLocale,
  matchLocale,
  type LocaleCode,
} from './locales'

export type { MessageKey }
export { SUPPORTED_LOCALES, type LocaleCode } from './locales'
export type Messages = Partial<Record<MessageKey, string>>
export type Params = Record<string, string | number>

/** Keys written as `<base>.other` (plus other CLDR categories) are plural families. */
export type PluralBase = MessageKey extends infer K ? (K extends `${infer B}.other` ? B : never) : never

// English ships in the main bundle. Other locales are separate chunks loaded on
// demand, so a user on English never downloads them. A key missing from the
// active catalog falls back to English, and a key missing there falls back to
// the key itself so a gap is visible but never throws.
const loaders: Record<Exclude<LocaleCode, 'en'>, () => Promise<{ default: Messages }>> = {
  'zh-CN': () => import('./zh-CN'),
}
const catalogs = new Map<string, Messages>([['en', en]])
const locale = ref<string>(DEFAULT_LOCALE)

export function registerLocale(code: string, messages: Messages): void {
  catalogs.set(code, messages)
}

function storage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null
  }
}

export function readStoredLocale(): LocaleCode | null {
  try {
    const v = storage()?.getItem(LOCALE_STORAGE_KEY)
    return isSupportedLocale(v) ? v : null
  } catch {
    return null
  }
}

function storeLocale(code: string): void {
  try {
    storage()?.setItem(LOCALE_STORAGE_KEY, code)
  } catch {
    // Persistence is best effort (private mode, blocked storage).
  }
}

/** Saved choice, then the browser languages, then English. Invalid saved values are ignored. */
export function detectLocale(): LocaleCode {
  const saved = readStoredLocale()
  if (saved) return saved
  const langs = typeof navigator === 'undefined' ? [] : navigator.languages?.length ? navigator.languages : [navigator.language]
  for (const tag of langs) {
    const m = tag ? matchLocale(tag) : null
    if (m) return m
  }
  return DEFAULT_LOCALE
}

/**
 * Switches the active locale, loading its catalog first when needed. Unknown
 * codes and failed loads leave the current locale untouched. Resolves to the
 * locale that is active afterwards.
 */
export async function setLocale(code: string, opts: { persist?: boolean } = {}): Promise<string> {
  if (!catalogs.has(code)) {
    if (!isSupportedLocale(code) || code === 'en') return locale.value
    try {
      catalogs.set(code, (await loaders[code]()).default)
    } catch {
      return locale.value
    }
  }
  locale.value = code
  if (typeof document !== 'undefined') document.documentElement.lang = code
  if (opts.persist) storeLocale(code)
  return locale.value
}

/** Detects and applies the initial locale; call and await before mounting the app. */
export function initLocale(): Promise<string> {
  return setLocale(detectLocale())
}

export function currentLocale(): string {
  return locale.value
}

/** Translate key, replacing {name} placeholders from params; unknown placeholders stay verbatim. */
export function t(key: MessageKey, params?: Params): string {
  const msg: string = catalogs.get(locale.value)?.[key] ?? en[key] ?? key
  if (!params) return msg
  return msg.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? String(params[name]) : m))
}

/** Plural-aware translate: picks `<base>.<category>` for the count, else `<base>.other`. `{count}` is filled in. */
export function tn(base: PluralBase, count: number, params?: Params): string {
  let cat = 'other'
  try {
    cat = new Intl.PluralRules(locale.value).select(count)
  } catch {
    // Unknown locale tag: use the generic form.
  }
  const specific = `${base}.${cat}` as MessageKey
  const has = (catalogs.get(locale.value) as Record<string, string> | undefined)?.[specific] !== undefined
    || (en as Record<string, string>)[specific] !== undefined
  return t(has ? specific : (`${base}.other` as MessageKey), { count, ...params })
}

export type Part = { text: string } | { slot: string }

/**
 * Splits a message into text and `{slot}` parts so a component can place inputs
 * inside a sentence ("Every {input} days") while the translation controls the order.
 */
export function tParts(key: MessageKey): Part[] {
  const msg: string = catalogs.get(locale.value)?.[key] ?? en[key] ?? key
  const out: Part[] = []
  for (const piece of msg.split(/(\{\w+\})/)) {
    if (piece === '') continue
    const m = /^\{(\w+)\}$/.exec(piece)
    out.push(m ? { slot: m[1]! } : { text: piece })
  }
  return out
}

/** Composable for components. `t` reads the reactive locale, so templates re-render on setLocale. */
export function useI18n() {
  return {
    locale: computed(() => locale.value),
    t: (key: MessageKey, params?: Params) => {
      void locale.value
      return t(key, params)
    },
    tn: (base: PluralBase, count: number, params?: Params) => {
      void locale.value
      return tn(base, count, params)
    },
    tParts: (key: MessageKey) => {
      void locale.value
      return tParts(key)
    },
  }
}
