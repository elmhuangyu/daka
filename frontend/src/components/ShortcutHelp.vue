<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n, type MessageKey } from '../i18n'
import { helpOpen } from '../composables/useGlobalShortcuts'
import { PRIVACY_TOGGLE_ID } from '../composables/shortcutCatalog'
import { bindingKeyParts } from '../composables/shortcutBindings'
import { shortcutPrefs } from '../composables/shortcutPrefs'
import { effectiveKeys, useShortcutList, type ShortcutScope } from '../composables/useShortcuts'

// Static text only; this panel must never read item, project or category data.
const { t } = useI18n()
const SCOPE_TITLES: Record<ShortcutScope, MessageKey> = {
  global: 'shortcuts.scope.global',
  today: 'shortcuts.scope.today',
  calendar: 'shortcuts.scope.calendar',
}
const SCOPE_ORDER: ShortcutScope[] = ['global', 'today', 'calendar']
const GENERAL_ROWS: { labelKey: MessageKey; keys: string[][] }[] = [
  { labelKey: 'shortcuts.help.moveBetween', keys: [['Tab'], ['Shift', 'Tab']] },
  { labelKey: 'shortcuts.help.closeDialogs', keys: [['Esc']] },
]

const list = useShortcutList()
const groups = computed(() =>
  SCOPE_ORDER.map((scope) => ({
    scope,
    title: t(SCOPE_TITLES[scope]),
    rows: list.value
      .filter((d) => d.scope === scope && (shortcutPrefs.enabled || d.id === PRIVACY_TOGGLE_ID))
      .map((d) => ({
        id: d.id,
        label: t(d.labelKey),
        keys: effectiveKeys(d).map(bindingKeyParts),
      })),
  })).filter((g) => g.rows.length > 0),
)

const box = ref<HTMLElement | null>(null)
let previous: HTMLElement | null = null

function close() {
  helpOpen.value = false
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
  }
}

function release() {
  window.removeEventListener('keydown', onKey)
  if (previous?.isConnected) previous.focus()
  previous = null
}

watch(
  helpOpen,
  async (open) => {
    if (open) {
      previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
      window.addEventListener('keydown', onKey)
      await nextTick()
      box.value?.focus()
    } else {
      release()
    }
  },
  { flush: 'post', immediate: true },
)
onBeforeUnmount(() => {
  // Unmounting while open (for example a redirect to /login) must not leave stale state behind.
  helpOpen.value = false
  release()
})
</script>

<template>
  <div
    v-if="helpOpen"
    class="modal modal-open"
    role="dialog"
    aria-modal="true"
    aria-labelledby="shortcut-help-title"
    data-testid="shortcut-help"
  >
    <div ref="box" class="modal-box" tabindex="-1">
      <h2 id="shortcut-help-title" class="text-lg font-bold">{{ t('shortcuts.title') }}</h2>
      <p
        v-if="!shortcutPrefs.enabled"
        class="mt-2 text-sm opacity-70"
        data-testid="shortcut-help-off"
      >
        {{ t('shortcuts.help.disabled') }}
      </p>
      <section v-for="g in groups" :key="g.scope" class="mt-4">
        <h3 class="text-sm font-semibold text-base-content/70">{{ g.title }}</h3>
        <ul class="mt-1 space-y-1">
          <li v-for="r in g.rows" :key="r.id" class="flex justify-between gap-4 text-sm">
            <span>{{ r.label }}</span>
            <span class="flex gap-2">
              <span v-if="r.keys.length === 0" class="opacity-60">{{
                t('shortcuts.unbound')
              }}</span>
              <span v-for="(seq, i) in r.keys" :key="i" class="flex gap-1">
                <kbd v-for="(k, j) in seq" :key="j" class="kbd kbd-sm">{{ k }}</kbd>
              </span>
            </span>
          </li>
        </ul>
      </section>
      <section class="mt-4">
        <h3 class="text-sm font-semibold text-base-content/70">
          {{ t('shortcuts.help.general') }}
        </h3>
        <ul class="mt-1 space-y-1">
          <li
            v-for="r in GENERAL_ROWS"
            :key="r.labelKey"
            class="flex justify-between gap-4 text-sm"
          >
            <span>{{ t(r.labelKey) }}</span>
            <span class="flex gap-2">
              <span v-for="(seq, i) in r.keys" :key="i" class="flex gap-1">
                <kbd v-for="(k, j) in seq" :key="j" class="kbd kbd-sm">{{ k }}</kbd>
              </span>
            </span>
          </li>
        </ul>
      </section>
      <div class="modal-action">
        <button type="button" class="btn btn-sm" data-testid="shortcut-help-close" @click="close">
          {{ t('common.close') }}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" data-testid="shortcut-help-backdrop" @click="close"></div>
  </div>
</template>
