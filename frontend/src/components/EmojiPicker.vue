<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { Icon } from '@iconify/vue'
import { useI18n } from '../i18n'

const { t, locale } = useI18n()

/** Optional emoji: '' means none. */
const model = defineModel<string>({ default: '' })
const props = defineProps<{ testid?: string }>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const toggleBtn = ref<HTMLButtonElement | null>(null)
const host = ref<HTMLElement | null>(null)
const loadError = ref(false)
const id = computed(() => props.testid ?? 'emoji-picker')

let picker: HTMLElement | null = null
let onClick: ((e: Event) => void) | null = null

function onDocMouseDown(e: MouseEvent) {
  if (!open.value || !root.value) return
  if (!root.value.contains(e.target as Node)) {
    open.value = false
  }
}

function onDocKeydown(e: KeyboardEvent) {
  if (!open.value || e.key !== 'Escape') return
  e.preventDefault()
  e.stopPropagation()
  open.value = false
  void nextTick(() => toggleBtn.value?.focus())
}

/**
 * The full Unicode set (search, skin tones, recents) comes from emoji-picker-element. Both the
 * element and its data are loaded only when the picker is first opened, and the data ships with the
 * app (no third-party requests), so it also works offline once cached. The Chinese data also carries
 * the English names (see emojiDataPlugin.ts), so English search works in both languages.
 */
async function ensurePicker() {
  if (picker || !host.value) return
  loadError.value = false
  try {
    const zh = locale.value.toLowerCase().startsWith('zh')
    const [{ Picker }, source, strings] = await Promise.all([
      import('emoji-picker-element'),
      zh
        ? import('virtual:emoji-zh-en-data')
        : import('emoji-picker-element-data/en/cldr/data.json?url'),
      zh ? import('emoji-picker-element/i18n/zh_CN') : import('emoji-picker-element/i18n/en'),
    ])
    if (!host.value) return
    const el = new Picker({
      dataSource: source.default,
      locale: zh ? 'zh' : 'en',
      i18n: strings.default,
    }) as unknown as HTMLElement
    el.className = 'daka-emoji-picker'
    onClick = (e: Event) => {
      const unicode = (e as CustomEvent<{ unicode?: string }>).detail?.unicode
      if (unicode) pick(unicode)
    }
    el.addEventListener('emoji-click', onClick)
    host.value.replaceChildren(el)
    picker = el
  } catch {
    loadError.value = true
  }
}

async function toggle() {
  open.value = !open.value
  if (open.value) {
    document.addEventListener('mousedown', onDocMouseDown)
    document.addEventListener('keydown', onDocKeydown, true)
    await nextTick()
    await ensurePicker()
  } else {
    document.removeEventListener('mousedown', onDocMouseDown)
    document.removeEventListener('keydown', onDocKeydown, true)
  }
}

function pick(e: string) {
  model.value = e
  open.value = false
  document.removeEventListener('mousedown', onDocMouseDown)
  document.removeEventListener('keydown', onDocKeydown, true)
  void nextTick(() => toggleBtn.value?.focus())
}

onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocMouseDown)
  document.removeEventListener('keydown', onDocKeydown, true)
  if (picker && onClick) picker.removeEventListener('emoji-click', onClick)
  picker = null
})

/** Keep the first grapheme cluster, so a pasted word or several emoji collapse to one. */
function firstGrapheme(s: string): string {
  const v = s.trim()
  if (!v) return ''
  const Seg = (Intl as unknown as { Segmenter?: new (l?: string, o?: object) => any }).Segmenter
  if (!Seg) return Array.from(v)[0] ?? ''
  const it = new Seg(undefined, { granularity: 'grapheme' }).segment(v)[Symbol.iterator]()
  return it.next().value?.segment ?? ''
}
function onInput(e: Event) {
  const el = e.target as HTMLInputElement
  const g = firstGrapheme(el.value)
  // Plain ASCII text is not an emoji (the server rejects it too).
  model.value = /^[\x20-\x7e]+$/.test(g) ? '' : g
  el.value = model.value
}
</script>

<template>
  <div ref="root" class="relative inline-block" :data-testid="id">
    <div class="flex items-center gap-2">
      <button
        ref="toggleBtn"
        type="button"
        class="btn btn-sm btn-outline min-w-12 text-lg leading-none"
        :aria-expanded="open"
        :aria-label="t('emoji.choose')"
        :title="t('emoji.choose')"
        :data-testid="`${id}-toggle`"
        @click="toggle"
      >
        <span v-if="model" aria-hidden="true">{{ model }}</span>
        <Icon v-else icon="lucide:smile-plus" class="size-4 opacity-60" aria-hidden="true" />
      </button>
      <button
        v-if="model"
        type="button"
        class="btn btn-ghost btn-xs"
        :data-testid="`${id}-clear`"
        @click="pick('')"
      >
        {{ t('emoji.none') }}
      </button>
    </div>

    <div
      v-show="open"
      class="absolute left-0 z-30 mt-2 w-[22rem] max-w-[calc(100vw-2rem)] space-y-2 rounded-xl border border-base-300 bg-base-100 p-2 shadow-lg"
      role="dialog"
      :aria-label="t('emoji.choose')"
    >
      <div ref="host" class="min-h-24" :data-testid="`${id}-host`"></div>
      <p v-if="loadError" class="px-1 text-xs text-error" role="alert">
        {{ t('emoji.loadFailed') }}
      </p>
      <input
        type="text"
        class="input input-bordered input-sm w-full"
        :placeholder="t('emoji.custom')"
        :aria-label="t('emoji.custom')"
        :data-testid="`${id}-input`"
        @change="onInput"
      />
    </div>
  </div>
</template>

<style>
.daka-emoji-picker {
  width: 100%;
  height: 20rem;
  --background: var(--color-base-100);
  --border-color: var(--color-base-300);
  --border-size: 0;
  --input-border-color: var(--color-base-300);
  --input-font-color: var(--color-base-content);
  --input-placeholder-color: color-mix(in oklab, var(--color-base-content) 50%, transparent);
  --category-font-color: var(--color-base-content);
  --button-hover-background: var(--color-base-200);
  --button-active-background: var(--color-base-300);
  --indicator-color: var(--color-primary);
  --outline-color: var(--color-primary);
  --num-columns: 8;
}
</style>
