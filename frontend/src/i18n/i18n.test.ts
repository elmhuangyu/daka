import { describe, expect, it } from 'vitest'
import { en } from './en'
import { currentLocale, registerLocale, setLocale, t, useI18n } from './index'

describe('i18n', () => {
  it('returns the english message and interpolates params', () => {
    expect(t('data.export.button')).toBe(en['data.export.button'])
    expect(t('data.delete.prompt', { word: 'DELETE' })).toBe('Type DELETE to confirm')
    expect(t('data.import.done', { items: 3, checkins: 0 })).toBe('Imported 3 items and 0 check-ins.')
  })

  it('keeps unknown placeholders verbatim', () => {
    expect(t('data.delete.prompt')).toBe('Type {word} to confirm')
    expect(t('data.delete.prompt', { other: 'x' })).toBe('Type {word} to confirm')
  })

  it('has no empty messages', () => {
    for (const [k, v] of Object.entries(en)) expect(v.trim(), k).not.toBe('')
  })

  it('falls back to english for keys a locale lacks, and switches reactively', async () => {
    registerLocale('xx', { 'data.export.button': 'XX export' })
    const { t: tt } = useI18n()
    await setLocale('xx')
    try {
      expect(currentLocale()).toBe('xx')
      expect(tt('data.export.button')).toBe('XX export')
      expect(tt('data.import.button')).toBe(en['data.import.button'])
    } finally {
      await setLocale('en')
    }
    expect(tt('data.export.button')).toBe(en['data.export.button'])
  })

  it('ignores unknown locales', async () => {
    await setLocale('nope')
    expect(currentLocale()).toBe('en')
  })
})
