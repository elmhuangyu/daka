<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import './icons'
import AppLayout from './components/AppLayout.vue'
import ReauthModal from './components/ReauthModal.vue'
import ShortcutHelp from './components/ShortcutHelp.vue'
import { useGlobalShortcuts } from './composables/useGlobalShortcuts'
import { catalogDef, PRIVACY_TOGGLE_ID } from './composables/shortcutCatalog'
import { installShortcutListener, useShortcut } from './composables/useShortcuts'
import { getCalendarSettings } from './api'
import { currentLocale, setLocale } from './i18n'
import { isSupportedLocale } from './i18n/locales'
import { useAuthStore } from './stores/auth'
import { useOfflineStore } from './stores/offline'
import { usePrivacyStore } from './stores/privacy'

const auth = useAuthStore()
const privacy = usePrivacyStore()
const offline = useOfflineStore()
const router = useRouter()
const route = useRoute()

let activationListenerInstalled = false
let removeActivationListener: (() => void) | null = null

function handleUnauthenticatedRedirect() {
  if (!auth.loginRequired) return

  // If running in a background tab (hidden), do not immediately bounce the user
  // to /login. Wait until the user focuses/activates this tab, and re-verify
  // auth status first (in case another tab logged back in or refreshed).
  if (typeof document !== 'undefined' && document.visibilityState === 'hidden') {
    if (activationListenerInstalled) return
    activationListenerInstalled = true

    const detach = () => {
      activationListenerInstalled = false
      removeActivationListener = null
      document.removeEventListener('visibilitychange', onVisible)
    }

    const onVisible = async () => {
      if (typeof document !== 'undefined' && document.visibilityState === 'visible') {
        detach()
        await auth.refreshStatus()
        if (!auth.authenticated && auth.loginRequired) {
          void router.replace({ name: 'login' })
        }
      }
    }
    removeActivationListener = detach
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

// The language is an account setting: once signed in, adopt the saved one on any device.
watch(
  () => auth.user?.id,
  async (id) => {
    if (!id) return
    try {
      const lang = (await getCalendarSettings()).language
      if (isSupportedLocale(lang) && lang !== currentLocale()) {
        await setLocale(lang, { persist: true })
      }
    } catch {
      // Keep the locally detected language.
    }
  },
  { immediate: true },
)

useShortcut(catalogDef(PRIVACY_TOGGLE_ID, () => privacy.toggle()))

useGlobalShortcuts()

let removeShortcutListener: (() => void) | null = null

onMounted(() => {
  removeShortcutListener = installShortcutListener()
  auth.listenBroadcast()
  offline.listen()
  void offline.start()
})
onBeforeUnmount(() => {
  removeShortcutListener?.()
  removeShortcutListener = null
  if (removeActivationListener) {
    removeActivationListener()
    removeActivationListener = null
  }
})
</script>

<template>
  <RouterView v-if="route.meta.public" />
  <AppLayout v-else>
    <main>
      <RouterView />
    </main>
  </AppLayout>
  <ReauthModal />
  <ShortcutHelp v-if="!route.meta.public" />
</template>
