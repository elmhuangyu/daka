import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import type { Plugin } from 'vite'

const require = createRequire(import.meta.url)
const ID = 'virtual:emoji-zh-en-data'
const RESOLVED = '\0' + ID
const DEV_URL = '/__emoji-zh-en.json'

interface Entry {
  emoji: string
  annotation?: string
  shortcodes?: string[]
  tags?: string[]
  [k: string]: unknown
}

const read = (p: string): Entry[] => JSON.parse(readFileSync(require.resolve(p), 'utf8'))

/** Chinese emoji data whose search terms also include the English names, so "pill" finds 💊. */
function merged(): string {
  const en = new Map(read('emoji-picker-element-data/en/cldr/data.json').map((e) => [e.emoji, e]))
  const out = read('emoji-picker-element-data/zh/cldr/data.json').map((e) => {
    const x = en.get(e.emoji)
    if (!x) return e
    const tags = new Set([...(e.tags ?? []), ...(x.tags ?? []), ...(x.shortcodes ?? [])])
    if (x.annotation) tags.add(x.annotation)
    return { ...e, tags: [...tags] }
  })
  return JSON.stringify(out)
}

export function emojiZhEnData(): Plugin {
  let isBuild = false
  return {
    name: 'daka-emoji-zh-en-data',
    configResolved(c) {
      isBuild = c.command === 'build'
    },
    resolveId: (id) => (id === ID ? RESOLVED : undefined),
    load(id) {
      if (id !== RESOLVED) return
      if (!isBuild) return `export default ${JSON.stringify(DEV_URL)}`
      const ref = this.emitFile({ type: 'asset', name: 'emoji-zh-en.json', source: merged() })
      return `export default import.meta.ROLLUP_FILE_URL_${ref}`
    },
    configureServer(server) {
      server.middlewares.use(DEV_URL, (_req, res) => {
        res.setHeader('Content-Type', 'application/json')
        res.end(merged())
      })
    },
  }
}
