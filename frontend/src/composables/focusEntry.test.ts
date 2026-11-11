import { afterEach, describe, expect, it } from 'vitest'
import { shouldClaimFocus } from './focusEntry'

afterEach(() => {
  document.body.innerHTML = ''
})

function add(html: string): HTMLElement {
  const host = document.createElement('div')
  host.innerHTML = html
  document.body.appendChild(host)
  return host
}

describe('shouldClaimFocus', () => {
  it('is true with nothing or body focused', () => {
    expect(shouldClaimFocus()).toBe(true)
    add('<button id="b">x</button>').querySelector('button')!.focus()
    ;(document.activeElement as HTMLElement).blur()
    expect(shouldClaimFocus()).toBe(true)
  })
  it('is true when focus is inside [data-nav]', () => {
    add('<nav data-nav><a href="#x" id="a">x</a></nav>')
    document.getElementById('a')!.focus()
    expect(shouldClaimFocus()).toBe(true)
  })
  it('is false for typing targets, even inside [data-nav]', () => {
    add('<input id="i" />')
    document.getElementById('i')!.focus()
    expect(shouldClaimFocus()).toBe(false)
    add('<nav data-nav><input id="j" /></nav>')
    document.getElementById('j')!.focus()
    expect(shouldClaimFocus()).toBe(false)
  })
  it('is false for other focused content', () => {
    add('<main><button id="m">x</button></main>')
    document.getElementById('m')!.focus()
    expect(shouldClaimFocus()).toBe(false)
  })
  it('is false while a modal is open', () => {
    add('<div role="dialog" aria-modal="true"></div>')
    expect(shouldClaimFocus()).toBe(false)
    add('<nav data-nav><a href="#x" id="a">x</a></nav>')
    document.getElementById('a')!.focus()
    expect(shouldClaimFocus()).toBe(false)
  })
})
