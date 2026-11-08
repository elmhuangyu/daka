import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import GeneralSection from './GeneralSection.vue'
import { currentLocale, setLocale } from '../../i18n'
import { LOCALE_STORAGE_KEY } from '../../i18n/locales'

beforeEach(() => {
  localStorage.clear()
  vi.stubGlobal('fetch', async () =>
    new Response(
      JSON.stringify({ timezone: 'UTC', week_start: 1, holiday_region: '', streak_holiday_freeze: false, external_calendars: [] }),
      { status: 200, headers: { 'Content-Type': 'application/json' } },
    ),
  )
})

afterEach(async () => {
  vi.unstubAllGlobals()
  await setLocale('en')
})

describe('GeneralSection language picker', () => {
  it('lists native labels, switches the UI language at once and remembers it per device', async () => {
    const w = mount(GeneralSection)
    await flushPromises()
    const picker = w.get('[data-testid=g-language]')
    expect(picker.findAll('option').map((o) => o.text())).toEqual(['English', '简体中文'])
    expect(w.text()).toContain('Time zone')

    await picker.setValue('zh-CN')
    await vi.waitFor(() => expect(currentLocale()).toBe('zh-CN'))
    await flushPromises()

    expect(w.text()).toContain('时区')
    expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh-CN')
    w.unmount()
  })
})
