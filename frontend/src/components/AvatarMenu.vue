<script setup lang="ts">
import { Icon } from '@iconify/vue'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const open = ref(false)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)

const initial = computed(() => (auth.user?.name ?? '?').trim().charAt(0).toUpperCase() || '?')

function onDocClick(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) open.value = false
}
function onKey(e: KeyboardEvent) {
  if (open.value && e.key === 'Escape') {
    open.value = false
    trigger.value?.focus()
  }
}
onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKey)
})

async function signOut() {
  open.value = false
  try {
    await auth.logout()
  } catch {
    // the store has already cleared local state; the router sends us to login
  }
}
</script>

<template>
  <div ref="root" class="relative">
    <button
      ref="trigger"
      type="button"
      class="btn btn-circle btn-sm btn-primary"
      aria-haspopup="menu"
      :aria-expanded="open"
      aria-label="Account menu"
      data-testid="avatar-button"
      @click="open = !open"
    >
      {{ initial }}
    </button>
    <div
      v-if="open"
      class="menu absolute right-0 z-30 mt-2 w-56 rounded-box bg-base-100 p-2 shadow"
      role="menu"
      data-testid="avatar-menu"
    >
      <p v-if="auth.user" class="px-3 py-2 text-sm font-semibold" data-testid="avatar-name">{{ auth.user.name }}</p>
      <button v-if="auth.loginRequired" type="button" role="menuitem" class="btn btn-ghost btn-sm justify-start" data-testid="logout" @click="signOut">
        <Icon icon="lucide:log-out" aria-hidden="true" /> Sign out
      </button>
      <p v-else class="px-3 py-1 text-xs opacity-70">Single-user mode: no sign-in</p>
    </div>
  </div>
</template>
