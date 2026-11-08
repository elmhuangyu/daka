<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { describeError, OIDC_LOGIN_URL } from '../api'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const secret = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await auth.loginWithSecret(secret.value)
    await router.replace('/')
  } catch (e) {
    error.value = describeError(e)
  } finally {
    busy.value = false
    secret.value = ''
  }
}
</script>

<template>
  <main class="mx-auto flex min-h-screen max-w-sm flex-col justify-center gap-4 p-4">
    <h1 class="text-2xl font-bold">Sign in to Daka</h1>
    <a v-if="auth.oidcEnabled" :href="OIDC_LOGIN_URL" class="btn btn-primary" data-testid="oidc-login">
      Sign in with your identity provider
    </a>
    <form v-else class="flex flex-col gap-3" @submit.prevent="submit">
      <label class="form-control w-full">
        <span class="label-text">Secret</span>
        <input
          v-model="secret"
          type="password"
          autocomplete="current-password"
          class="input input-bordered w-full"
          required
        />
      </label>
      <p v-if="error" class="error" role="alert" data-testid="login-error">{{ error }}</p>
      <button type="submit" class="btn btn-primary" :disabled="busy">Sign in</button>
    </form>
    <p v-if="auth.initError" class="error" role="alert">{{ auth.initError }}</p>
  </main>
</template>
