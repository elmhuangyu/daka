/**
 * English message catalog, the source of truth for message keys. Add keys here
 * first; other locales are partial overrides of it (see index.ts).
 * Placeholders use {name}.
 */
export const en = {
  'data.title': 'Your data',
  'data.intro': 'Export everything you have recorded, import a previous export, or delete your account. These actions ask you to confirm your identity again.',
  'data.export.title': 'Export',
  'data.export.help': 'Downloads one JSON file with your settings, categories, items with their full rule history, check-ins and freezes. Calendar feed URLs and tokens are not included. The file is unencrypted and may contain sensitive items, so keep it safe.',
  'data.export.button': 'Download export',
  'data.export.done': 'Export downloaded.',
  'data.import.title': 'Import',
  'data.import.help': 'Imported data is added next to your existing data; nothing is overwritten or merged.',
  'data.import.choose': 'Export file',
  'data.import.applySettings': 'Also apply time zone, week start and holiday settings from the file',
  'data.import.summary': 'Ready to import: {items} items, {options} options, {versions} rule versions, {checkins} check-ins, {categories} categories, {freezes} freezes.',
  'data.import.button': 'Import',
  'data.import.done': 'Imported {items} items and {checkins} check-ins.',
  'data.import.tooLarge': 'The file is larger than {mb} MB.',
  'data.import.readFailed': 'Could not read the file.',
  'data.delete.title': 'Delete account',
  'data.delete.help': 'Permanently deletes your account and all your data: items, check-ins, categories, tokens, Telegram link and settings. This cannot be undone. Export your data first if you may need it.',
  'data.delete.prompt': 'Type {word} to confirm',
  'data.delete.button': 'Delete my account',
  'data.delete.done': 'Your account was deleted.',
} as const

export type MessageKey = keyof typeof en
