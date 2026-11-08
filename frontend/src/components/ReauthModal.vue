<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { describeError } from '../api'
import { useAuthStore } from '../stores/auth'
import { useI18n } from '../i18n'

const { t } = useI18n()

const auth = useAuthStore()
const secret = ref('')
const error = ref('')
const busy = ref(false)
const input = ref<HTMLInputElement | null>(null)

watch(
  () => auth.reauthOpen,
  async (open) => {
    secret.value = ''
    error.value = ''
    if (open) {
      await nextTick()
      input.value?.focus()
    }
  },
)

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    if (auth.oidcEnabled) await auth.reauthWithOidc()
    else await auth.reauthWithSecret(secret.value)
  } catch (e) {
    error.value = describeError(e)
  } finally {
    busy.value = false
    secret.value = ''
  }
}
</script>

<template>
  <div
    v-if="auth.reauthOpen"
    class="modal modal-open"
    role="dialog"
    aria-modal="true"
    aria-labelledby="reauth-title"
    data-testid="reauth-modal"
    @keydown.esc="auth.settleReauth(false)"
  >
    <form class="modal-box" @submit.prevent="submit">
      <h2 id="reauth-title" class="text-lg font-bold">{{ t('reauth.title') }}</h2>
      <p class="py-2 text-sm">{{ t('reauth.body') }}</p>
      <label v-if="!auth.oidcEnabled" class="form-control w-full">
        <span class="label-text">{{ t('reauth.secret') }}</span>
        <input
          ref="input"
          v-model="secret"
          type="password"
          autocomplete="current-password"
          class="input input-bordered w-full"
          required
        />
      </label>
      <p v-else class="text-sm">{{ t('reauth.redirect') }}</p>
      <p v-if="error" class="error mt-2" role="alert" data-testid="reauth-error">{{ error }}</p>
      <div class="modal-action">
        <button type="button" class="btn" @click="auth.settleReauth(false)">{{ t('common.cancel') }}</button>
        <button type="submit" class="btn btn-primary" :disabled="busy">
          {{ auth.oidcEnabled ? t('reauth.continue') : t('reauth.confirm') }}
        </button>
      </div>
    </form>
  </div>
</template>
