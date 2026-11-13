import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '../api'
import { useOfflineStore } from './offline'

export const useAuthStore = defineStore('auth', () => {
  const ready = ref(false)
  const initError = ref('')
  const oidcEnabled = ref(false)
  /** False in single-user mode without a shared secret: there is no login at all. */
  const loginRequired = ref(false)
  const authenticated = ref(false)
  const user = ref<{ id: string; name: string; email?: string; avatar_url?: string } | null>(null)

  let initPromise: Promise<void> | null = null

  async function loadUser() {
    try {
      const me = await api.getMe()
      user.value = { id: me.id, name: me.name, email: me.email, avatar_url: me.avatar_url }
      await useOfflineStore().bindUser(me.id)
    } catch {
      user.value = null
    }
  }

  async function refreshStatus() {
    try {
      const st = await api.getAuthStatus()
      oidcEnabled.value = !!st.oidc_enabled
      loginRequired.value = !!st.login_required
      authenticated.value = !!st.authenticated
      initError.value = ''
      if (authenticated.value) await loadUser()
      else user.value = null
    } catch (e) {
      initError.value = api.describeError(e)
    } finally {
      ready.value = true
    }
  }

  /** Loads /api/auth/status once; later calls reuse the result. */
  function init(): Promise<void> {
    initPromise ??= refreshStatus()
    return initPromise
  }

  function markUnauthenticated() {
    if (!loginRequired.value) return
    authenticated.value = false
    user.value = null
  }

  async function loginWithSecret(secret: string) {
    await api.login(secret)
    authenticated.value = true
    await loadUser()
  }

  async function logout() {
    try {
      await useOfflineStore().clear()
      await api.logout()
    } finally {
      authenticated.value = false
      user.value = null
    }
  }

  api.setAuthHooks({ onUnauthorized: markUnauthenticated })

  let unsubscribeBroadcast: (() => void) | null = null
  function listenBroadcast() {
    if (unsubscribeBroadcast || typeof window === 'undefined') return
    unsubscribeBroadcast = api.onAuthBroadcast(async (msg) => {
      if (msg.type === 'logged_in') {
        await refreshStatus()
      } else if (msg.type === 'logged_out' || msg.type === 'session_expired') {
        markUnauthenticated()
      }
    })
  }

  return {
    ready,
    initError,
    oidcEnabled,
    loginRequired,
    authenticated,
    user,
    init,
    refreshStatus,
    markUnauthenticated,
    loginWithSecret,
    logout,
    listenBroadcast,
  }
})
