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
  const user = ref<{ id: string; name: string; email?: string } | null>(null)

  // Reauth modal state. Concurrent requests share one prompt.
  const reauthOpen = ref(false)
  let reauthWaiters: Array<(ok: boolean) => void> = []

  let initPromise: Promise<void> | null = null

  async function loadUser() {
    try {
      const me = await api.getMe()
      user.value = { id: me.id, name: me.name, email: me.email }
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

  function requestReauth(): Promise<boolean> {
    return new Promise((resolve) => {
      reauthWaiters.push(resolve)
      reauthOpen.value = true
    })
  }

  function settleReauth(ok: boolean) {
    const w = reauthWaiters
    reauthWaiters = []
    reauthOpen.value = false
    for (const r of w) r(ok)
  }

  /** Secret-mode reauth. Settles true on success; throws ApiError otherwise. */
  async function reauthWithSecret(secret: string) {
    await api.reauth(secret)
    settleReauth(true)
  }

  /** OIDC reauth: asks the server for the redirect target and navigates there. */
  async function reauthWithOidc() {
    const r = await api.reauth()
    if (r.redirect) {
      settleReauth(false)
      window.location.assign(r.redirect)
    } else {
      settleReauth(true)
    }
  }

  api.setAuthHooks({ onUnauthorized: markUnauthenticated, onReauthRequired: requestReauth })

  return {
    ready, initError, oidcEnabled, loginRequired, authenticated, user, reauthOpen,
    init, refreshStatus, loginWithSecret, logout, requestReauth, settleReauth, reauthWithSecret, reauthWithOidc,
  }
})
