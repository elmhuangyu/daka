import { describe, expect, it } from 'vitest'
import { parse } from 'vue/compiler-sfc'

// Guards against hard-coded user-facing text in templates: every visible text
// node and every text-bearing attribute must come from t(), not a literal.

// Every component and view, as raw source text.
const FILES = import.meta.glob<string>('../**/*.vue', {
  query: '?raw',
  import: 'default',
  eager: true,
})
const ATTRS = new Set(['placeholder', 'title', 'aria-label', 'alt', 'label'])
// Literal text that is not translatable prose.
const ALLOWED_TEXT = new Set(['Daka'])
const LETTERS = /\p{L}{2,}/u
// Bot commands such as /start are protocol text, not prose.
const COMMAND = /^\/\w+$/

interface Node {
  type: number
  tag?: string
  content?: string | Node
  props?: { type: number; name: string; value?: { content: string } }[]
  children?: Node[]
}

function scan(node: Node, found: string[]): void {
  if (node.type === 2 && typeof node.content === 'string') {
    const text = node.content.trim()
    if (LETTERS.test(text) && !ALLOWED_TEXT.has(text) && !COMMAND.test(text))
      found.push(`text "${text}"`)
  }
  if (node.type === 1) {
    for (const p of node.props ?? []) {
      if (p.type === 6 && ATTRS.has(p.name) && p.value && LETTERS.test(p.value.content)) {
        found.push(`<${node.tag} ${p.name}="${p.value.content}">`)
      }
    }
  }
  for (const c of node.children ?? []) scan(c, found)
}

describe('templates have no hard-coded UI text', () => {
  for (const [file, source] of Object.entries(FILES)) {
    it(`${file.replace('../', '')}`, () => {
      const { descriptor } = parse(source, { filename: file })
      const found: string[] = []
      if (descriptor.template?.ast) scan(descriptor.template.ast as unknown as Node, found)
      expect(found).toEqual([])
    })
  }
})
