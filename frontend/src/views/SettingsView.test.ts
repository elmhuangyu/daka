import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import { setLocale } from './../i18n'
import { createPinia, setActivePinia } from 'pinia'
import { makeRouter } from '../router'
import SettingsView from './SettingsView.vue'
import { usePrivacyStore } from '../stores/privacy'
import { MASK_STORAGE_KEY, useSettingsStore } from '../stores/settings'

function jsonRes(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let calls: {
  url: string
  method: string
  body?: Record<string, unknown> & { external_calendars?: unknown }
}[]
let telegramConfigured = true
let telegramBotUsername = 'dakabot'
let telegramLinked = false

const cal = {
  timezone: 'Asia/Shanghai',
  week_start: 1,
  holiday_region: '',
  streak_holiday_freeze: false,
  external_calendars: [{ id: 'work', name: 'Work', url_hint: 'example.com/…/a.ics' }],
}

beforeEach(() => {
  localStorage.clear()
  calls = []
  telegramConfigured = true
  telegramBotUsername = 'dakabot'
  telegramLinked = false
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (url === '/api/auth/status')
      return jsonRes({ oidc_enabled: false, login_required: false, authenticated: true })
    if (url === '/api/settings/calendar') return jsonRes(cal)
    if (url === '/api/categories' && method === 'GET') {
      return jsonRes({
        categories: [
          { id: 1, name: 'Health', color: '', icon: '', is_sensitive: true },
          { id: 2, name: 'Work', color: '', icon: '', is_sensitive: false },
        ],
      })
    }
    if (url.startsWith('/api/categories/') && method === 'PUT')
      return jsonRes({ id: 1, name: 'Health', is_sensitive: false, affected_items: 2 })
    if (url.startsWith('/api/categories/') && method === 'DELETE')
      return jsonRes({ deleted: true, moved_items: 0, affected_items: 0 })
    if (url === '/api/tokens' && method === 'GET') return jsonRes({ tokens: [] })
    if (url === '/api/tokens' && method === 'POST') {
      return jsonRes(
        { id: 1, name: 'cal', token: 'dk_secret', feed_path: '/feed.ics?token=dk_secret' },
        201,
      )
    }
    if (url === '/api/telegram' && method === 'GET') {
      return jsonRes({
        configured: telegramConfigured,
        linked: telegramLinked,
        enabled: false,
        remind_sensitive: true,
        digest_time: '08:00',
        timezone: 'Asia/Shanghai',
        lang: '',
        linked_at: telegramLinked ? '2026-11-08T00:00:00Z' : null,
        bot_username: telegramBotUsername,
      })
    }
    if (url === '/api/telegram' && method === 'PUT') {
      telegramBotUsername = 'my_new_bot'
      telegramLinked = true
      return jsonRes({
        configured: true,
        linked: true,
        enabled: true,
        remind_sensitive: true,
        digest_time: '08:00',
        linked_at: '2026-11-08T00:00:00Z',
        bot_username: 'my_new_bot',
      })
    }
    if (url === '/api/telegram/settings' && method === 'PATCH') {
      return jsonRes({
        configured: true,
        linked: true,
        enabled: true,
        remind_sensitive: true,
        digest_time: '08:00',
        timezone: 'Asia/Shanghai',
        lang: JSON.parse(String(init?.body)).lang ?? '',
        linked_at: '2026-11-08T00:00:00Z',
        bot_username: 'dakabot',
      })
    }
    if (url === '/api/telegram/test' && method === 'POST') {
      return jsonRes({ ok: true, sent: true })
    }
    if (url === '/api/telegram/link-code')
      return jsonRes({ error: 'telegram is not configured' }, 503)
    return jsonRes({})
  })
})

async function mountView() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = makeRouter(createMemoryHistory())
  await router.push('/settings')
  await router.isReady()
  const w = mount(SettingsView, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return w
}

describe('SettingsView', () => {
  it('shows the shortcuts tab', async () => {
    const w = await mountView()
    expect(w.find('[data-testid=tab-shortcuts]').exists()).toBe(true)
    await w.get('[data-testid=tab-shortcuts]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid=section-shortcuts]').isVisible()).toBe(true)
  })

  it('saves general settings via PUT', async () => {
    const w = await mountView()
    await w.get('[data-testid=g-week-start]').setValue(0)
    await w.get('[data-testid=g-region]').setValue('CN')
    await w.get('[data-testid=section-general] form').trigger('submit')
    await flushPromises()
    const put = calls.find((c) => c.method === 'PUT' && c.url === '/api/settings/calendar')!
    expect(put.body).toMatchObject({
      timezone: 'Asia/Shanghai',
      week_start: 0,
      holiday_region: 'CN',
    })
  })

  it('asks before un-marking a category and sends confirm_exposure when exposing', async () => {
    const w = await mountView()
    await w.get('[data-testid=cat-toggle]').trigger('click')
    expect(w.find('[data-testid=cat-unmark-dialog]').exists()).toBe(true)
    expect(calls.some((c) => c.method === 'PUT' && c.url.startsWith('/api/categories/'))).toBe(
      false,
    )
    await w.get('[data-testid=cat-expose]').setValue(true)
    await w.get('[data-testid=cat-unmark-confirm]').trigger('click')
    await flushPromises()
    const put = calls.find((c) => c.method === 'PUT' && c.url.startsWith('/api/categories/'))!
    expect(put.url).toBe('/api/categories/1?confirm_exposure=1')
    expect(put.body).toEqual({ is_sensitive: false })
    expect(w.get('[data-testid=cat-notice]').text()).toContain('2 items are now exposed')
  })

  it('deletes a category moving its items', async () => {
    const w = await mountView()
    await w.findAll('[data-testid=cat-delete]')[1].trigger('click')
    await w.get('[data-testid=cat-delete-target]').setValue(1)
    await w.get('[data-testid=cat-delete-confirm]').trigger('click')
    await flushPromises()
    expect(calls.find((c) => c.method === 'DELETE')!.url).toBe('/api/categories/2?move_to=1')
  })

  it('masks sensitive category names while the guard is on', async () => {
    const w = await mountView()
    expect(w.text()).toContain('Health')
    usePrivacyStore().set(true)
    await flushPromises()
    expect(w.text()).not.toContain('Health')
    expect(w.text()).toContain('Sensitive category')
    // Opting out of masking shows the name again; the flag persists.
    useSettingsStore().setMaskCategories(false)
    await flushPromises()
    expect(w.text()).toContain('Health')
    expect(localStorage.getItem(MASK_STORAGE_KEY)).toBe('0')
  })

  it('shows the token plaintext once and clears it on close', async () => {
    const w = await mountView()
    await w.get('[data-testid=tok-name]').setValue('cal')
    await w.get('[data-testid=section-tokens] form').trigger('submit')
    await flushPromises()
    const input = w.get('[data-testid=tok-plain]').element as HTMLInputElement
    expect(input.value).toContain('/feed.ics?token=dk_secret')
    await w.get('[data-testid=tok-close]').trigger('click')
    expect(w.find('[data-testid=tok-dialog]').exists()).toBe(false)
    expect(w.html()).not.toContain('dk_secret')
    expect(JSON.stringify(Object.entries(localStorage))).not.toContain('dk_secret')
  })

  it('adds and removes external calendars without resending stored URLs', async () => {
    const w = await mountView()
    expect(w.get('[data-testid=ext-hint]').text()).toBe('example.com/…/a.ics')
    await w.get('[data-testid=ext-name]').setValue('Home')
    await w.get('[data-testid=ext-url]').setValue('https://example.com/h.ics')
    await w.get('[data-testid=section-external] form').trigger('submit')
    await flushPromises()
    let put = calls.filter((c) => c.method === 'PUT' && c.url === '/api/settings/calendar').at(-1)!
    expect(put.body!.external_calendars).toEqual([
      { id: 'work', name: 'Work' },
      { name: 'Home', url: 'https://example.com/h.ics' },
    ])
    await w.get('[data-testid=ext-remove]').trigger('click')
    await flushPromises()
    put = calls.filter((c) => c.method === 'PUT' && c.url === '/api/settings/calendar').at(-1)!
    expect(put.body!.external_calendars).toEqual([])
  })

  it('shows bot not configured when the link code request returns 503', async () => {
    const w = await mountView()
    await w.get('[data-testid=tg-connect]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid=tg-unconfigured]').text()).toContain('not configured')
  })

  it('shows bot not configured from the status', async () => {
    telegramConfigured = false
    const w = await mountView()
    expect(w.find('[data-testid=tg-unconfigured]').exists()).toBe(true)
    expect(w.find('[data-testid=tg-connect]').exists()).toBe(false)
  })

  it('saves bot token and links telegram bot', async () => {
    telegramBotUsername = ''
    const w = await mountView()
    expect(w.find('[data-testid=tg-bot-token]').exists()).toBe(true)
    await w.get('[data-testid=tg-bot-token]').setValue('123:abc')
    await w.get('[data-testid=tg-chat-id]').setValue('999')
    await w.get('[data-testid=tg-save-bot]').trigger('submit')
    await flushPromises()

    const putCall = calls.find((c) => c.url === '/api/telegram' && c.method === 'PUT')
    expect(putCall).toBeDefined()
    expect(putCall?.body).toEqual({ bot_token: '123:abc', chat_id: 999 })
    expect(w.find('[data-testid=tg-linked]').exists()).toBe(true)
  })

  it('shows the digest time zone and stores the UI language for bot messages', async () => {
    telegramLinked = true
    await setLocale('zh-CN', { persist: false })
    const w = await mountView()
    await flushPromises()
    expect(w.get('[data-testid=tg-digest-tz]').text()).toContain('Asia/Shanghai')
    const p = calls.find((c) => c.url === '/api/telegram/settings' && c.method === 'PATCH')
    expect(p?.body).toEqual({ lang: 'zh-CN' })
    await setLocale('en', { persist: false })
  })

  it('sends test notification when linked', async () => {
    telegramLinked = true
    const w = await mountView()
    expect(w.find('[data-testid=tg-test]').exists()).toBe(true)
    await w.get('[data-testid=tg-test]').trigger('click')
    await flushPromises()

    const testCall = calls.find((c) => c.url === '/api/telegram/test' && c.method === 'POST')
    expect(testCall).toBeDefined()
    expect(w.find('[data-testid=tg-test-success]').exists()).toBe(true)
  })
})
