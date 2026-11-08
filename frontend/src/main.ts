import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { makeRouter } from './router'
import './style.css'

createApp(App).use(createPinia()).use(makeRouter()).mount('#app')

if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch(() => {
      // The app works without the service worker.
    })
  })
}
