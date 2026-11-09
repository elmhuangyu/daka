<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { describeError, OIDC_LOGIN_URL } from '../api'
import { useAuthStore } from '../stores/auth'
import { useI18n } from '../i18n'

const { t } = useI18n()

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
  <main class="mx-auto flex min-h-screen max-w-md flex-col justify-center p-4">
    <div class="card card-border bg-base-100 border-base-300 p-6 sm:p-8 shadow-xl">
      <div class="flex flex-col items-center text-center gap-2 mb-6">
        <div
          class="size-12 rounded-2xl bg-primary text-primary-content flex items-center justify-center font-bold text-2xl shadow-sm mb-1"
        >
          D
        </div>
        <h1 class="text-2xl font-bold tracking-tight">{{ t('login.title') }}</h1>
      </div>

      <a
        v-if="auth.oidcEnabled"
        :href="OIDC_LOGIN_URL"
        class="btn btn-primary w-full"
        data-testid="oidc-login"
      >
        {{ t('login.oidc') }}
      </a>
      <form v-else class="flex flex-col gap-4" @submit.prevent="submit">
        <div class="flex flex-col gap-1.5 w-full">
          <label class="text-sm font-medium text-base-content/80" for="login-secret">{{
            t('login.secret')
          }}</label>
          <input
            id="login-secret"
            v-model="secret"
            type="password"
            autocomplete="current-password"
            class="input input-bordered w-full"
            required
          />
        </div>
        <p
          v-if="error"
          class="alert alert-error text-sm py-2"
          role="alert"
          data-testid="login-error"
        >
          {{ error }}
        </p>
        <button type="submit" class="btn btn-primary w-full" :disabled="busy">
          {{ t('login.submit') }}
        </button>
      </form>
      <p v-if="auth.initError" class="alert alert-error text-sm py-2 mt-4" role="alert">
        {{ auth.initError }}
      </p>
    </div>
  </main>
</template>
