/** Locales the UI ships. Labels are native names so a user can find their language in any UI language. */
export const SUPPORTED_LOCALES = [
  { value: 'en', label: 'English' },
  { value: 'zh-CN', label: '简体中文' },
] as const

export type LocaleCode = (typeof SUPPORTED_LOCALES)[number]['value']

export const DEFAULT_LOCALE: LocaleCode = 'en'
export const LOCALE_STORAGE_KEY = 'daka_locale'

export function isSupportedLocale(code: unknown): code is LocaleCode {
  return typeof code === 'string' && SUPPORTED_LOCALES.some((l) => l.value === code)
}

/** Maps a BCP 47 tag from navigator.languages to a supported locale, or null. */
export function matchLocale(tag: string): LocaleCode | null {
  const lower = tag.toLowerCase()
  // zh-Hant / zh-TW / zh-HK also land on zh-CN until a Traditional locale exists;
  // a future zh-TW locale must branch on Hant (and TW/HK/MO) before this check.
  if (lower === 'zh' || lower.startsWith('zh-')) return 'zh-CN'
  if (lower === 'en' || lower.startsWith('en-')) return 'en'
  return null
}
