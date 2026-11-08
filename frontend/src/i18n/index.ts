import { computed, ref } from 'vue'
import { en, type MessageKey } from './en'

export type { MessageKey }
export type Messages = Partial<Record<MessageKey, string>>
export type Params = Record<string, string | number>

// English is the only shipped locale. Other locales register a partial catalog
// with registerLocale(); missing keys fall back to English, and a key missing
// there falls back to the key itself so a gap is visible but never throws.
const catalogs = new Map<string, Messages>([['en', en]])
const locale = ref('en')

export function registerLocale(code: string, messages: Messages): void {
  catalogs.set(code, messages)
}

export function setLocale(code: string): void {
  if (catalogs.has(code)) locale.value = code
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

/** Composable for components. `t` reads the reactive locale, so templates re-render on setLocale. */
export function useI18n() {
  return {
    locale: computed(() => locale.value),
    t: (key: MessageKey, params?: Params) => {
      void locale.value
      return t(key, params)
    },
  }
}
