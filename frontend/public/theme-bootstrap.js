// Applies the persisted theme before the app bundle loads, so the first paint
// already uses the right palette. Served as an external file (not inline) so the
// site-wide Content-Security-Policy can forbid inline scripts.
try {
  var theme = localStorage.getItem('theme')
  if (theme && /^[a-z0-9-_]+$/.test(theme)) {
    document.documentElement.setAttribute('data-theme', theme)
  } else {
    var prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
    document.documentElement.setAttribute('data-theme', prefersDark ? 'dark' : 'light')
  }
} catch {
  /* localStorage can be unavailable; the default theme applies */
}
