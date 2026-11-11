import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import EmojiPicker from './EmojiPicker.vue'

// The real element needs IndexedDB; a plain element stands in for it.
vi.mock('emoji-picker-element', () => ({
  Picker: class {
    constructor() {
      return document.createElement('div')
    }
  },
}))

async function open(w: ReturnType<typeof mount>, id = 'e') {
  await w.get(`[data-testid=${id}-toggle]`).trigger('click')
  await flushPromises()
}

describe('EmojiPicker', () => {
  it('is optional: empty by default, picking from the element sets it, clear removes it', async () => {
    const w = mount(EmojiPicker, {
      props: { modelValue: '', testid: 'e' },
      attachTo: document.body,
    })
    expect(w.find('[data-testid=e-clear]').exists()).toBe(false)
    await open(w)
    // The element and its data are imported lazily on first open.
    const el = await vi.waitFor(() => {
      const c = w.get('[data-testid=e-host]').element.firstElementChild
      if (!c) throw new Error('picker not mounted yet')
      return c
    })
    el.dispatchEvent(new CustomEvent('emoji-click', { detail: { unicode: '🥤' } }))
    expect(w.emitted('update:modelValue')!.at(-1)).toEqual(['🥤'])
    await w.setProps({ modelValue: '🥤' })
    await w.get('[data-testid=e-clear]').trigger('click')
    expect(w.emitted('update:modelValue')!.at(-1)).toEqual([''])
    w.unmount()
  })

  it('accepts a pasted emoji, keeps one, and rejects plain text', async () => {
    const w = mount(EmojiPicker, {
      props: { modelValue: '', testid: 'e' },
      attachTo: document.body,
    })
    await open(w)
    const input = w.get('[data-testid=e-input]')
    await input.setValue('🥤🍺')
    await input.trigger('change')
    expect(w.emitted('update:modelValue')!.at(-1)).toEqual(['🥤'])
    await input.setValue('cola')
    await input.trigger('change')
    expect(w.emitted('update:modelValue')!.at(-1)).toEqual([''])
    w.unmount()
  })
})
