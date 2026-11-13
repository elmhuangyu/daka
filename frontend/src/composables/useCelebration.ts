// Randomised completion celebrations. Pure DOM + Web Animations API, no dependencies.
// Every effect is skipped when the user prefers reduced motion or the API is unavailable.

export type Point = { x: number; y: number }
export type EffectName = 'confetti' | 'sparkles' | 'emoji' | 'ripple' | 'fireworks'

const COLORS = [
  'var(--color-primary)',
  'var(--color-secondary)',
  'var(--color-accent)',
  'var(--color-success)',
  'var(--color-warning)',
  'var(--color-info)',
]
const EMOJIS = ['🎉', '✨', '🌟', '💪', '🔥', '🌈', '🍀', '🚀', '🎯', '👏', '💫', '🌸']
const SPARKLES = ['✦', '✧', '★', '✨']

const rand = (min: number, max: number) => min + Math.random() * (max - min)
const pick = <T>(xs: readonly T[]): T => xs[Math.floor(Math.random() * xs.length)]!

let last: Point = { x: 0, y: 0 }
let tracking = false

/** Remember where the user last clicked or pressed so bursts start from the button they hit. */
export function trackPointer() {
  if (tracking || typeof window === 'undefined') return
  tracking = true
  const rec = (e: PointerEvent) => {
    last = { x: e.clientX, y: e.clientY }
  }
  window.addEventListener('pointerdown', rec, { capture: true, passive: true })
}

function enabled(): boolean {
  if (typeof document === 'undefined' || typeof Element === 'undefined') return false
  if (typeof Element.prototype.animate !== 'function') return false
  return !window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
}

function layer(): HTMLElement {
  const el = document.createElement('div')
  el.setAttribute('aria-hidden', 'true')
  el.style.cssText = 'position:fixed;inset:0;pointer-events:none;z-index:9999;overflow:hidden'
  document.body.appendChild(el)
  return el
}

function particle(host: HTMLElement, css: string, text = ''): HTMLElement {
  const el = document.createElement('span')
  el.style.cssText = `position:absolute;left:0;top:0;will-change:transform,opacity;${css}`
  el.textContent = text
  host.appendChild(el)
  return el
}

function run(host: HTMLElement, anims: Animation[]) {
  const done = Promise.allSettled(anims.map((a) => a.finished))
  void done.then(() => host.remove())
  // Safety net in case an animation is cancelled without settling.
  window.setTimeout(() => host.remove(), 4000)
}

function origin(p?: Point): Point {
  if (p) return p
  if (last.x || last.y) return last
  return { x: window.innerWidth / 2, y: window.innerHeight / 3 }
}

function confetti(o: Point, count = 28): void {
  const host = layer()
  const anims: Animation[] = []
  for (let i = 0; i < count; i++) {
    const w = rand(6, 11)
    const el = particle(
      host,
      `width:${w}px;height:${w * rand(0.4, 1)}px;background:${pick(COLORS)};border-radius:${Math.random() < 0.3 ? '50%' : '2px'}`,
    )
    const ang = rand(-Math.PI * 0.95, -Math.PI * 0.05)
    const v = rand(80, 220)
    const dx = Math.cos(ang) * v
    const dy = Math.sin(ang) * v
    anims.push(
      el.animate(
        [
          { transform: `translate(${o.x}px,${o.y}px) rotate(0deg)`, opacity: 1 },
          {
            transform: `translate(${o.x + dx}px,${o.y + dy}px) rotate(${rand(90, 360)}deg)`,
            opacity: 1,
            offset: 0.45,
          },
          {
            transform: `translate(${o.x + dx * 1.3}px,${o.y + dy * 0.2 + rand(120, 260)}px) rotate(${rand(360, 900)}deg)`,
            opacity: 0,
          },
        ],
        { duration: rand(900, 1500), easing: 'cubic-bezier(.2,.7,.4,1)', fill: 'forwards' },
      ),
    )
  }
  run(host, anims)
}

function sparkles(o: Point): void {
  const host = layer()
  const anims: Animation[] = []
  const n = 12
  for (let i = 0; i < n; i++) {
    const el = particle(
      host,
      `font-size:${rand(12, 24)}px;color:${pick(COLORS)};line-height:1;margin:-8px 0 0 -8px`,
      pick(SPARKLES),
    )
    const ang = (i / n) * Math.PI * 2 + rand(-0.2, 0.2)
    const d = rand(40, 100)
    anims.push(
      el.animate(
        [
          { transform: `translate(${o.x}px,${o.y}px) scale(0) rotate(0deg)`, opacity: 1 },
          {
            transform: `translate(${o.x + Math.cos(ang) * d}px,${o.y + Math.sin(ang) * d}px) scale(1.2) rotate(120deg)`,
            opacity: 1,
            offset: 0.6,
          },
          {
            transform: `translate(${o.x + Math.cos(ang) * d * 1.2}px,${o.y + Math.sin(ang) * d * 1.2}px) scale(0) rotate(240deg)`,
            opacity: 0,
          },
        ],
        { duration: rand(700, 1000), easing: 'ease-out', fill: 'forwards' },
      ),
    )
  }
  run(host, anims)
}

function emoji(o: Point): void {
  const host = layer()
  const anims: Animation[] = []
  const set = [pick(EMOJIS), pick(EMOJIS), pick(EMOJIS)]
  for (let i = 0; i < 7; i++) {
    const el = particle(
      host,
      `font-size:${rand(20, 34)}px;line-height:1;margin:-14px 0 0 -14px`,
      pick(set),
    )
    const dx = rand(-70, 70)
    anims.push(
      el.animate(
        [
          { transform: `translate(${o.x}px,${o.y}px) scale(.3)`, opacity: 0 },
          {
            transform: `translate(${o.x + dx * 0.4}px,${o.y - 40}px) scale(1.1)`,
            opacity: 1,
            offset: 0.2,
          },
          {
            transform: `translate(${o.x + dx}px,${o.y - rand(110, 190)}px) scale(.9) rotate(${rand(-30, 30)}deg)`,
            opacity: 0,
          },
        ],
        { duration: rand(1100, 1600), delay: i * 50, easing: 'ease-out', fill: 'forwards' },
      ),
    )
  }
  run(host, anims)
}

function ripple(o: Point): void {
  const host = layer()
  const anims: Animation[] = []
  const color = pick(COLORS)
  for (let i = 0; i < 3; i++) {
    const el = particle(
      host,
      `width:40px;height:40px;margin:-20px 0 0 -20px;border-radius:50%;border:3px solid ${color}`,
    )
    anims.push(
      el.animate(
        [
          { transform: `translate(${o.x}px,${o.y}px) scale(.2)`, opacity: 0.9 },
          { transform: `translate(${o.x}px,${o.y}px) scale(${3.5 + i})`, opacity: 0 },
        ],
        { duration: 800 + i * 150, delay: i * 140, easing: 'ease-out', fill: 'forwards' },
      ),
    )
  }
  run(host, anims)
}

function fireworks(o: Point): void {
  const host = layer()
  const anims: Animation[] = []
  const shots = 2
  for (let s = 0; s < shots; s++) {
    const cx = o.x + (s === 0 ? 0 : rand(-90, 90))
    const cy = o.y - (s === 0 ? rand(60, 100) : rand(90, 160))
    const color = pick(COLORS)
    const n = 16
    for (let i = 0; i < n; i++) {
      const el = particle(
        host,
        `width:5px;height:5px;margin:-2px 0 0 -2px;border-radius:50%;background:${color};box-shadow:0 0 6px ${color}`,
      )
      const ang = (i / n) * Math.PI * 2
      const d = rand(45, 85)
      anims.push(
        el.animate(
          [
            { transform: `translate(${o.x}px,${o.y}px) scale(.6)`, opacity: 1 },
            { transform: `translate(${cx}px,${cy}px) scale(1)`, opacity: 1, offset: 0.35 },
            {
              transform: `translate(${cx + Math.cos(ang) * d}px,${cy + Math.sin(ang) * d + 30}px) scale(.2)`,
              opacity: 0,
            },
          ],
          { duration: 1100, delay: s * 250, easing: 'ease-out', fill: 'forwards' },
        ),
      )
    }
  }
  run(host, anims)
}

const EFFECTS: Record<EffectName, (o: Point) => void> = {
  confetti: (o) => confetti(o),
  sparkles,
  emoji,
  ripple,
  fireworks,
}
const NAMES = Object.keys(EFFECTS) as EffectName[]
let lastEffect: EffectName | null = null

/** Play one randomly chosen effect, never the same one twice in a row. */
export function celebrate(at?: Point, force?: EffectName): EffectName | null {
  if (!enabled()) return null
  const choices = NAMES.filter((n) => n !== lastEffect)
  const name = force ?? pick(choices)
  lastEffect = name
  EFFECTS[name](origin(at))
  return name
}

/** Bigger finale for finishing everything: a confetti rain plus bursts from both sides. */
export function celebrateAll(): void {
  if (!enabled()) return
  const w = window.innerWidth
  const h = window.innerHeight
  confetti({ x: w * 0.2, y: h * 0.75 }, 40)
  window.setTimeout(() => confetti({ x: w * 0.8, y: h * 0.75 }, 40), 180)
  window.setTimeout(() => fireworks({ x: w / 2, y: h * 0.6 }), 400)
  window.setTimeout(() => emoji({ x: w / 2, y: h * 0.7 }), 700)
}
