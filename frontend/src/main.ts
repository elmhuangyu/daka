import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { makeRouter } from './router'
import { initLocale } from './i18n'
import './style.css'

// Load the saved locale before mounting so the first paint is already translated.
await initLocale()

try {
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme && /^[a-z0-9-_]+$/.test(savedTheme)) {
    document.documentElement.setAttribute('data-theme', savedTheme)
  }
} catch {
  // localStorage may be unavailable
}

createApp(App).use(createPinia()).use(makeRouter()).mount('#app')

if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch(() => {
      // The app works without the service worker.
    })
  })
}
