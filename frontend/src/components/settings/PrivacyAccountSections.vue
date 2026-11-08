<script setup lang="ts">
import { computed, ref, watch } from 'vue'
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
const imgFailed = ref(false)

watch(
  () => auth.user?.avatar_url,
  () => {
    imgFailed.value = false
  },
)

const showAvatar = computed(() => !!auth.user?.avatar_url && !imgFailed.value)

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
      <div v-if="auth.user" class="flex items-center gap-3">
        <div class="size-12 shrink-0 overflow-hidden rounded-full bg-primary text-primary-content flex items-center justify-center font-bold text-base">
          <img
            v-if="showAvatar"
            :src="auth.user.avatar_url"
            :alt="auth.user.name"
            class="h-full w-full object-cover"
            data-testid="account-avatar-image"
            @error="imgFailed = true"
          />
          <span v-else data-testid="account-avatar-initial">{{ (auth.user.name ?? '?').trim().charAt(0).toUpperCase() || '?' }}</span>
        </div>
        <div>
          <p class="font-semibold">{{ auth.user.name }}</p>
          <p v-if="auth.user.email" class="text-sm opacity-80">{{ auth.user.email }}</p>
        </div>
      </div>
      <p v-if="!auth.loginRequired" class="text-sm opacity-80" data-testid="claim-hint">
        This instance has no login configured, so anyone who can reach it has access. Set a shared secret or OIDC
        before exposing it beyond localhost.
      </p>
      <p v-if="error" class="text-error" role="alert">{{ error }}</p>
      <button v-if="auth.loginRequired" class="btn btn-outline self-start" data-testid="logout" @click="logout">Log out</button>
    </div>
  </section>
</template>
