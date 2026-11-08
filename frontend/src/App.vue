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

// A 401 mid-session flips `authenticated`; send the user to the login page.
watch(
  () => auth.authenticated,
  (ok) => {
    if (!ok && auth.loginRequired) void router.replace({ name: 'login' })
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
