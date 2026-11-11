import { afterEach, describe, expect, it } from 'vitest'
import { effectScope } from 'vue'
import { mount } from '@vue/test-utils'
import { en } from '../i18n/en'
import zhCN from '../i18n/zh-CN'
import { setLocale, t } from '../i18n'
import { catalogDef, SHORTCUT_CATALOG } from '../composables/shortcutCatalog'
import { helpOpen } from '../composables/useGlobalShortcuts'
import { useShortcut } from '../composables/useShortcuts'
import ShortcutHelp from './ShortcutHelp.vue'

afterEach(async () => {
  helpOpen.value = false
  await setLocale('en')
})

describe('shortcut translations', () => {
  it('resolves every catalog label in both locales', () => {
    for (const e of SHORTCUT_CATALOG) {
      expect((en as Record<string, string>)[e.labelKey]).toBeTruthy()
      expect((zhCN as Record<string, string>)[e.labelKey]).toBeTruthy()
    }
  })

  it('re-renders the help panel when the locale changes', async () => {
    const scope = effectScope()
    scope.run(() => useShortcut(catalogDef('search.focus', () => {})))
    helpOpen.value = true
    const w = mount(ShortcutHelp, { attachTo: document.body })
    expect(w.text()).toContain('Keyboard shortcuts')
    expect(w.text()).toContain('Focus search')
    await setLocale('zh-CN')
    await w.vm.$nextTick()
    expect(w.text()).toContain(t('shortcuts.title'))
    expect(w.text()).toContain('键盘快捷键')
    expect(w.text()).toContain('聚焦搜索')
    expect(w.text()).not.toContain('Focus search')
    w.unmount()
    scope.stop()
  })
})
