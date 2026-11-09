import { afterEach, describe, expect, it, vi } from 'vitest'
import { en } from './en'
import zhCN from './zh-CN'
import {
  SUPPORTED_LOCALES,
  currentLocale,
  detectLocale,
  initLocale,
  registerLocale,
  setLocale,
  t,
  tn,
  tParts,
  useI18n,
} from './index'
import { LOCALE_STORAGE_KEY } from './locales'

function mockStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial))
  const store = {
    getItem: vi.fn((k: string) => data.get(k) ?? null),
    setItem: vi.fn((k: string, v: string) => void data.set(k, v)),
  }
  vi.stubGlobal('localStorage', store)
  return { data, store }
}

function mockLanguages(langs: string[]) {
  vi.stubGlobal('navigator', { languages: langs, language: langs[0] ?? '' })
}

afterEach(() => {
  vi.unstubAllGlobals()
  document.documentElement.lang = ''
})

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]!).sort()

describe('catalogs', () => {
  it('zh-CN has exactly the keys of en', () => {
    expect(Object.keys(zhCN).sort()).toEqual(Object.keys(en).sort())
  })

  it('has no empty values', () => {
    for (const [k, v] of Object.entries(en)) expect(v.trim(), `en ${k}`).not.toBe('')
    for (const [k, v] of Object.entries(zhCN)) expect(v.trim(), `zh-CN ${k}`).not.toBe('')
  })

  it('uses the same placeholders in every locale', () => {
    for (const k of Object.keys(en) as (keyof typeof en)[]) {
      expect(placeholders(zhCN[k]), k).toEqual(placeholders(en[k]))
    }
  })

  it('every plural family has the other form', () => {
    for (const k of Object.keys(en)) {
      const m = /^(.*)\.(one|two|few|many|zero)$/.exec(k)
      if (m) expect(en, `${m[1]}.other`).toHaveProperty(`${m[1]}.other`)
    }
  })

  it('lists each supported locale once with a native label', () => {
    expect(SUPPORTED_LOCALES.map((l) => l.value)).toEqual(['en', 'zh-CN'])
    expect(SUPPORTED_LOCALES.map((l) => l.label)).toEqual(['English', '简体中文'])
  })
})

describe('t', () => {
  it('returns the english message and interpolates params', () => {
    expect(t('data.export.button')).toBe(en['data.export.button'])
    expect(t('data.delete.prompt', { word: 'DELETE' })).toBe('Type DELETE to confirm')
    expect(t('data.import.done', { items: 3, checkins: 0 })).toBe(
      'Imported 3 items and 0 check-ins.',
    )
  })

  it('keeps unknown placeholders verbatim', () => {
    expect(t('data.delete.prompt')).toBe('Type {word} to confirm')
    expect(t('data.delete.prompt', { other: 'x' })).toBe('Type {word} to confirm')
  })

  it('splits a message into text and slot parts', () => {
    expect(tParts('items.editor.everyNDays')).toEqual([
      { text: 'Every ' },
      { slot: 'input' },
      { text: ' days' },
    ])
  })
})

describe('detectLocale', () => {
  it('prefers the saved locale over the browser languages', () => {
    mockStorage({ [LOCALE_STORAGE_KEY]: 'en' })
    mockLanguages(['zh-CN'])
    expect(detectLocale()).toBe('en')
  })

  it('maps any zh browser language to zh-CN', () => {
    mockStorage()
    for (const tag of ['zh', 'zh-CN', 'zh-TW', 'zh-Hans-CN']) {
      mockLanguages([tag])
      expect(detectLocale(), tag).toBe('zh-CN')
    }
  })

  it('uses the first matching browser language', () => {
    mockStorage()
    mockLanguages(['fr-FR', 'zh-CN', 'en'])
    expect(detectLocale()).toBe('zh-CN')
    mockLanguages(['fr', 'en-GB', 'zh'])
    expect(detectLocale()).toBe('en')
  })

  it('falls back to en when nothing matches', () => {
    mockStorage()
    mockLanguages(['fr', 'de'])
    expect(detectLocale()).toBe('en')
    mockLanguages([])
    expect(detectLocale()).toBe('en')
  })

  it('ignores an invalid saved locale', () => {
    mockStorage({ [LOCALE_STORAGE_KEY]: 'klingon' })
    mockLanguages(['zh-CN'])
    expect(detectLocale()).toBe('zh-CN')
  })

  it('survives storage that throws', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('blocked')
      },
      setItem: () => {
        throw new Error('blocked')
      },
    })
    mockLanguages(['zh-CN'])
    expect(detectLocale()).toBe('zh-CN')
  })
})

describe('setLocale', () => {
  it('lazy loads zh-CN, switches reactively, sets <html lang> and persists on request', async () => {
    const { data } = mockStorage()
    const { t: tt, locale } = useI18n()
    expect(tt('nav.today')).toBe('Today')
    await expect(setLocale('zh-CN', { persist: true })).resolves.toBe('zh-CN')
    try {
      expect(currentLocale()).toBe('zh-CN')
      expect(locale.value).toBe('zh-CN')
      expect(tt('nav.today')).toBe(zhCN['nav.today'])
      expect(document.documentElement.lang).toBe('zh-CN')
      expect(data.get(LOCALE_STORAGE_KEY)).toBe('zh-CN')
    } finally {
      await setLocale('en')
    }
    expect(tt('nav.today')).toBe('Today')
    expect(document.documentElement.lang).toBe('en')
  })

  it('ignores a stale load that finishes after a newer choice', async () => {
    // A fresh module instance has an empty catalog cache, so zh-CN really awaits the chunk.
    vi.resetModules()
    let release!: () => void
    const gate = new Promise<void>((r) => (release = r))
    vi.doMock('./zh-CN', async () => {
      await gate
      return { default: { 'nav.today': '今天' } }
    })
    try {
      const fresh = await import('./index')
      const { data } = mockStorage()
      const slow = fresh.setLocale('zh-CN', { persist: true })
      await fresh.setLocale('en', { persist: true })
      release()
      await slow
      expect(fresh.currentLocale()).toBe('en')
      expect(document.documentElement.lang).toBe('en')
      expect(data.get(LOCALE_STORAGE_KEY)).toBe('en')
    } finally {
      vi.doUnmock('./zh-CN')
      vi.resetModules()
    }
  })

  it('does not persist unless asked', async () => {
    const { store } = mockStorage()
    await setLocale('zh-CN')
    await setLocale('en')
    expect(store.setItem).not.toHaveBeenCalled()
  })

  it('ignores unknown locales', async () => {
    await expect(setLocale('nope')).resolves.toBe('en')
    expect(currentLocale()).toBe('en')
  })

  it('falls back to english for keys a locale lacks', async () => {
    registerLocale('xx', { 'data.export.button': 'XX export' })
    const { t: tt } = useI18n()
    await setLocale('xx')
    try {
      expect(tt('data.export.button')).toBe('XX export')
      expect(tt('data.import.button')).toBe(en['data.import.button'])
    } finally {
      await setLocale('en')
    }
    expect(tt('data.export.button')).toBe(en['data.export.button'])
  })

  it('initLocale applies the detected locale', async () => {
    mockStorage({ [LOCALE_STORAGE_KEY]: 'zh-CN' })
    mockLanguages(['en'])
    try {
      await expect(initLocale()).resolves.toBe('zh-CN')
      expect(t('nav.settings')).toBe(zhCN['nav.settings'])
    } finally {
      await setLocale('en')
    }
  })
})

describe('tn (plurals)', () => {
  it('picks one/other for english', () => {
    expect(tn('common.days', 1)).toBe('1 day')
    expect(tn('common.days', 0)).toBe('0 days')
    expect(tn('common.days', 5)).toBe('5 days')
  })

  it('passes extra params and uses the active locale', async () => {
    expect(tn('rrule.count', 1, { head: 'x' })).toBe('x, 1 time in total')
    expect(tn('rrule.count', 3, { head: 'x' })).toBe('x, 3 times in total')
    await setLocale('zh-CN')
    try {
      expect(tn('common.days', 1)).toBe('1 天')
      expect(tn('rrule.count', 3, { head: 'x' })).toBe('x，共 3 次')
    } finally {
      await setLocale('en')
    }
  })
})
