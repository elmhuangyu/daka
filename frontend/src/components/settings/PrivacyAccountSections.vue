<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { describeError } from '../../api'
import { useAuthStore } from '../../stores/auth'
import { usePrivacyStore } from '../../stores/privacy'
import { useSettingsStore } from '../../stores/settings'

const settings = useSettingsStore()
const privacy = usePrivacyStore()
const auth = useAuthStore()
const router = useRouter()
const error = ref('')

async function logout() {
  error.value = ''
  try {
    await auth.logout()
    await router.push({ name: 'login' })
  } catch (e) {
    error.value = describeError(e)
  }
}
</script>

<template>
  <section class="card bg-base-200" data-testid="section-privacy">
    <div class="card-body gap-3">
      <h2 class="card-title">Privacy</h2>
      <p class="text-sm opacity-80">
        The privacy guard is currently <strong>{{ privacy.redact ? 'on' : 'off' }}</strong>. Toggle it from the top bar.
      </p>
      <label class="label cursor-pointer justify-start gap-3">
        <input
          type="checkbox" class="checkbox" :checked="settings.maskCategories" data-testid="p-mask"
          @change="settings.setMaskCategories(($event.target as HTMLInputElement).checked)"
        />
        <span class="label-text">Mask sensitive category names while the privacy guard is on</span>
      </label>
    </div>
  </section>
  <section class="card bg-base-200" data-testid="section-account">
    <div class="card-body gap-3">
      <h2 class="card-title">Account</h2>
      <p v-if="auth.user">Signed in as {{ auth.user.name }}<template v-if="auth.user.email"> ({{ auth.user.email }})</template>.</p>
      <p v-if="!auth.loginRequired" class="text-sm opacity-80" data-testid="claim-hint">
        This instance has no login configured, so anyone who can reach it has access. Set a shared secret or OIDC
        before exposing it beyond localhost.
      </p>
      <p v-if="error" class="text-error" role="alert">{{ error }}</p>
      <button v-if="auth.loginRequired" class="btn btn-outline self-start" data-testid="logout" @click="logout">Log out</button>
    </div>
  </section>
</template>
