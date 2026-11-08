<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import './icons'
import AppLayout from './components/AppLayout.vue'
import ReauthModal from './components/ReauthModal.vue'
import { useAuthStore } from './stores/auth'
import { useOfflineStore } from './stores/offline'
import { usePrivacyStore } from './stores/privacy'

const auth = useAuthStore()
const privacy = usePrivacyStore()
const offline = useOfflineStore()
const router = useRouter()
const route = useRoute()

let activationListenerInstalled = false

function handleUnauthenticatedRedirect() {
  if (!auth.loginRequired) return

  // If running in a background tab (hidden), do not immediately bounce the user
  // to /login. Wait until the user focuses/activates this tab, and re-verify
  // auth status first (in case another tab logged back in or refreshed).
  if (typeof document !== 'undefined' && document.visibilityState === 'hidden') {
    if (activationListenerInstalled) return
    activationListenerInstalled = true

    const onVisible = async () => {
      if (typeof document !== 'undefined' && document.visibilityState === 'visible') {
        activationListenerInstalled = false
        document.removeEventListener('visibilitychange', onVisible)
        await auth.refreshStatus()
        if (!auth.authenticated && auth.loginRequired) {
          void router.replace({ name: 'login' })
        }
      }
    }
    document.addEventListener('visibilitychange', onVisible)
    return
  }

  void router.replace({ name: 'login' })
}

// A 401 mid-session flips `authenticated`; send the user to the login page.
watch(
  () => auth.authenticated,
  (ok) => {
    if (!ok) handleUnauthenticatedRedirect()
  },
)

function onKey(e: KeyboardEvent) {
  if (!e.repeat && e.ctrlKey && e.shiftKey && e.key.toLowerCase() === 'h') {
    e.preventDefault()
    privacy.toggle()
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKey)
  auth.listenBroadcast()
  offline.listen()
  void offline.start()
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <RouterView v-if="route.meta.public" />
  <AppLayout v-else>
    <main>
      <RouterView />
    </main>
  </AppLayout>
  <ReauthModal />
</template>
