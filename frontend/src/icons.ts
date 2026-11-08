import { addIcon } from '@iconify/vue'

// Icons are bundled (Lucide paths) instead of fetched from the Iconify API at
// runtime: no third-party requests, and they work offline.
const wrap = (body: string) =>
  `<g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${body}</g>`

const ICONS: Record<string, string> = {
  'lucide:calendar-check': wrap(
    '<path d="M8 2v4M16 2v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18m-11 6 2 2 4-4"/>',
  ),
  'lucide:calendar': wrap('<path d="M8 2v4m8-4v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18"/>'),
  'lucide:list-checks': wrap('<path d="m3 17 2 2 4-4M3 7l2 2 4-4M13 6h8M13 12h8M13 18h8"/>'),
  'lucide:settings': wrap(
    '<path d="M21 4h-7m-4 0H3m18 8h-9m-4 0H3m18 8h-5m-4 0H3M14 2v4m-6 6v4m8 4v4"/>',
  ),
  'lucide:eye': wrap(
    '<path d="M2.06 12.35a1 1 0 0 1 0-.7 10.75 10.75 0 0 1 19.88 0 1 1 0 0 1 0 .7 10.75 10.75 0 0 1-19.88 0"/><circle cx="12" cy="12" r="3"/>',
  ),
  'lucide:eye-off': wrap(
    '<path d="M10.73 5.08A10.43 10.43 0 0 1 12 5c7 0 10 7 10 7a13.16 13.16 0 0 1-1.67 2.68M6.61 6.61A13.53 13.53 0 0 0 2 12s3 7 10 7a9.74 9.74 0 0 0 5.39-1.61M2 2l20 20"/>',
  ),
  'lucide:log-out': wrap('<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4m7 14 5-5-5-5m5 5H9"/>'),
}

for (const [name, body] of Object.entries(ICONS)) addIcon(name, { body, width: 24, height: 24 })
