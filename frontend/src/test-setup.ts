import { beforeEach } from 'vitest'
import { setLocale } from './i18n'

// Tests assert English text; pin the locale so a saved choice never leaks between tests.
beforeEach(async () => {
  await setLocale('en')
})
