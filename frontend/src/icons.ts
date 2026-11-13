import { addIcon } from '@iconify/vue'

// Icons are bundled (Lucide paths) instead of fetched from the Iconify API at
// runtime: no third-party requests, and they work offline.
const wrap = (body: string) =>
  `<g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${body}</g>`

const ICONS: Record<string, string> = {
  'lucide:keyboard': wrap(
    '<path d="M10 8h.01M12 12h.01M14 8h.01M16 12h.01M18 8h.01M6 8h.01M7 16h10M8 12h.01"/><rect width="20" height="16" x="2" y="4" rx="2"/>',
  ),
  'lucide:smile-plus': wrap(
    '<path d="M22 11v1a10 10 0 1 1-9-10"/><path d="M8 14s1.5 2 4 2 4-2 4-2"/><path d="M9 9h.01M15 9h.01M16 5h6M19 2v6"/>',
  ),
  'lucide:calendar-check': wrap(
    '<path d="M8 2v4M16 2v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18m-11 6 2 2 4-4"/>',
  ),
  'lucide:cloud-off': wrap(
    '<path d="m2 2 20 20M5.782 5.782A7 7 0 0 0 9 19h8.5a4.5 4.5 0 0 0 1.307-.193M21.532 16.5A4.5 4.5 0 0 0 17.5 10h-1.79A7 7 0 0 0 8.1 4.1"/>',
  ),
  'lucide:calendar': wrap(
    '<path d="M8 2v4m8-4v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18"/>',
  ),
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
  'lucide:plus': wrap('<path d="M5 12h14m-7-7v14"/>'),
  'lucide:trash-2': wrap(
    '<path d="M3 6h18m-2 0v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6m3 0V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2m-6 5v6m4-6v6"/>',
  ),
  'lucide:log-out': wrap('<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4m7 14 5-5-5-5m5 5H9"/>'),
  'lucide:minus': wrap('<path d="M5 12h14"/>'),
  'lucide:circle': wrap('<circle cx="12" cy="12" r="10"/>'),
  'lucide:corner-down-right': wrap('<path d="m15 10 5 5-5 5"/><path d="M4 4v7a4 4 0 0 0 4 4h12"/>'),
  'lucide:gauge': wrap('<path d="m12 14 4-4"/><path d="M3.34 19a10 10 0 1 1 17.32 0"/>'),
  'lucide:target': wrap(
    '<circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/>',
  ),
  'lucide:check': wrap('<path d="M20 6 9 17l-5-5"/>'),
  'lucide:menu': wrap('<path d="M4 6h16M4 12h16M4 18h16"/>'),
  'lucide:x': wrap('<path d="M18 6 6 18M6 6l12 12"/>'),
  'lucide:undo-2': wrap(
    '<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 5.5 5.5a5.5 5.5 0 0 1-5.5 5.5H11"/>',
  ),
  'lucide:triangle-alert': wrap(
    '<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3M12 9v4m0 4h.01"/>',
  ),
  'lucide:copy': wrap(
    '<rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
  ),
  'lucide:lock': wrap(
    '<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
  ),
  'lucide:search': wrap('<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>'),
  'lucide:pin': wrap(
    '<path d="M12 17v5m-3-12.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>',
  ),
  'lucide:archive': wrap(
    '<rect width="20" height="5" x="2" y="3" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8m-10 4h4"/>',
  ),
  'lucide:pencil': wrap(
    '<path d="M21.2 5.9 18.1 2.8a1.9 1.9 0 0 0-2.7 0L3 15.2V21h5.8L21.2 8.6a1.9 1.9 0 0 0 0-2.7M15 5l4 4"/>',
  ),
  'lucide:chevron-left': wrap('<path d="m15 18-6-6 6-6"/>'),
  'lucide:chevron-right': wrap('<path d="m9 18 6-6-6-6"/>'),
  'lucide:sliders': wrap(
    '<line x1="4" x2="4" y1="21" y2="14"/><line x1="4" x2="4" y1="10" y2="3"/><line x1="12" x2="12" y1="21" y2="12"/><line x1="12" x2="12" y1="8" y2="3"/><line x1="20" x2="20" y1="21" y2="16"/><line x1="20" x2="20" y1="12" y2="3"/><line x1="1" x2="7" y1="14" y2="14"/><line x1="9" x2="15" y1="8" y2="8"/><line x1="17" x2="23" y1="16" y2="16"/>',
  ),
  'lucide:sliders-horizontal': wrap(
    '<line x1="21" x2="14" y1="4" y2="4"/><line x1="10" x2="3" y1="4" y2="4"/><line x1="21" x2="12" y1="12" y2="12"/><line x1="8" x2="3" y1="12" y2="12"/><line x1="21" x2="16" y1="20" y2="20"/><line x1="12" x2="3" y1="20" y2="20"/><line x1="14" x2="14" y1="2" y2="6"/><line x1="8" x2="8" y1="10" y2="14"/><line x1="16" x2="16" y1="18" y2="22"/>',
  ),
  'lucide:tags': wrap(
    '<path d="m15 5 6.3 6.3a2.4 2.4 0 0 1 0 3.4L17 19a2.4 2.4 0 0 1-3.4 0L7.3 12.7A2.4 2.4 0 0 1 7 11V5a2 2 0 0 1 2-2h6a2 2 0 0 1 1.7 1z"/><path d="M7 7h.01"/><path d="m2 9 6.3 6.3a2.4 2.4 0 0 0 3.4 0L14 13"/>',
  ),
  'lucide:link-2': wrap(
    '<path d="M9 17H7A5 5 0 0 1 7 7h2"/><path d="M15 7h2a5 5 0 1 1 0 10h-2"/><line x1="8" x2="16" y1="12" y2="12"/>',
  ),
  'lucide:shield': wrap(
    '<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/>',
  ),
  'lucide:database': wrap(
    '<ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/>',
  ),
  'lucide:download': wrap(
    '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/>',
  ),
  'lucide:upload': wrap(
    '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" x2="12" y1="3" y2="15"/>',
  ),
  'lucide:key': wrap(
    '<path d="m15.5 7.5 2.3 2.3a1 1 0 0 0 1.4 0l2.1-2.1a1 1 0 0 0 0-1.4L19 4"/><path d="m21 2-9.6 9.6"/><circle cx="7.5" cy="15.5" r="5.5"/>',
  ),
  'lucide:send': wrap('<path d="m22 2-7 20-4-9-9-4Z"/><path d="M22 2 11 13"/>'),
  'lucide:user': wrap(
    '<path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>',
  ),
  'lucide:info': wrap('<circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/>'),
  'lucide:sparkles': wrap(
    '<path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z"/><path d="M5 3v4"/><path d="M19 17v4"/><path d="M3 5h4"/><path d="M17 19h4"/>',
  ),
  'lucide:chevron-down': wrap('<path d="m6 9 6 6 6-6"/>'),
  'lucide:calendar-days': wrap(
    '<rect width="18" height="18" x="3" y="4" rx="2"/><path d="M8 2v4m8-4v4M3 10h18"/><path d="M8 14h.01M12 14h.01M16 14h.01M8 18h.01M12 18h.01M16 18h.01"/>',
  ),
  'lucide:check-circle-2': wrap('<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>'),
  'lucide:filter': wrap('<polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/>'),
  'lucide:folder': wrap(
    '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
  ),
  'lucide:folder-plus': wrap(
    '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/><path d="M12 11v6m-3-3h6"/>',
  ),
  'lucide:folder-check': wrap(
    '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/><path d="m9 13 2 2 4-4"/>',
  ),
  'lucide:folder-archive': wrap(
    '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/><path d="M12 10v5m-2.5-2.5 2.5 2.5 2.5-2.5"/>',
  ),
  'lucide:folder-open': wrap(
    '<path d="m6 14 1.45-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.55 6a2 2 0 0 1-1.94 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.93a2 2 0 0 1 1.66.9l.82 1.2a2 2 0 0 0 1.66.9H18a2 2 0 0 1 2 2v2"/>',
  ),
  'lucide:list-plus': wrap('<path d="M11 12H3m13-6H3m7 12H3m13 0h6m-3-3v6"/>'),
  'lucide:repeat': wrap(
    '<path d="m17 2 4 4-4 4"/><path d="M3 11v-1a4 4 0 0 1 4-4h14"/><path d="m7 22-4-4 4-4"/><path d="M21 13v1a4 4 0 0 1-4 4H3"/>',
  ),
  'lucide:unlink': wrap(
    '<path d="m18.84 12.25 1.72-1.71a4.5 4.5 0 0 0-6.36-6.36l-1.72 1.71"/><path d="m5.16 11.75-1.72 1.71a4.5 4.5 0 0 0 6.36 6.36l1.72-1.71"/><line x1="2" x2="22" y1="2" y2="22"/>',
  ),
}

for (const [name, body] of Object.entries(ICONS)) addIcon(name, { body, width: 24, height: 24 })
