<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import ReauthModal from './components/ReauthModal.vue'
import { useAuthStore } from './stores/auth'

const STORAGE_KEY = 'daka.redact'

function loadRedact(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    return false
  }
}

const auth = useAuthStore()
const router = useRouter()
// A 401 mid-session flips `authenticated`; send the user to the login page.
watch(
  () => auth.authenticated,
  (ok) => {
    if (!ok && auth.loginRequired) void router.replace({ name: 'login' })
  },
)

const redact = ref(loadRedact())

function toggleRedact() {
  redact.value = !redact.value
  try {
    localStorage.setItem(STORAGE_KEY, redact.value ? '1' : '0')
  } catch {
    // storage unavailable; keep the in-memory value only
  }
}

function onKey(e: KeyboardEvent) {
  if (!e.repeat && e.ctrlKey && e.shiftKey && e.key.toLowerCase() === 'h') {
    e.preventDefault()
    toggleRedact()
  }
}

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <main>
    <div class="row">
      <button type="button" data-testid="redact-toggle" :aria-pressed="redact" @click="toggleRedact">
        Privacy: {{ redact ? 'on' : 'off' }}
      </button>
    </div>
    <RouterView v-slot="{ Component }">
      <component :is="Component" :redact="redact" />
    </RouterView>
  </main>
  <ReauthModal />
</template>
