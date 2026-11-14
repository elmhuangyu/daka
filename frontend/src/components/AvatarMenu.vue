<script setup lang="ts">
import { Icon } from '@iconify/vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useI18n } from '../i18n'

const { t } = useI18n()

const auth = useAuthStore()
const open = ref(false)
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const imgFailed = ref(false)

// Reset image failure flag when the user or avatar_url changes.
watch(
  () => auth.user?.avatar_url,
  () => {
    imgFailed.value = false
  },
)

const initial = computed(() => (auth.user?.name ?? '?').trim().charAt(0).toUpperCase() || '?')
const showImage = computed(() => !!auth.user?.avatar_url && !imgFailed.value)

function onImgError() {
  imgFailed.value = true
}

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
  <div ref="root" class="relative" data-nav>
    <button
      ref="trigger"
      type="button"
      class="inline-flex items-center gap-2 rounded-full border border-base-300/70 bg-base-200/40 hover:bg-base-200/80 p-1 sm:pr-2.5 text-sm font-medium transition-colors cursor-pointer shadow-2xs focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
      aria-haspopup="menu"
      :aria-expanded="open"
      :aria-label="t('layout.accountMenu')"
      data-testid="avatar-button"
      @click="open = !open"
    >
      <span
        class="size-7 shrink-0 overflow-hidden rounded-full bg-primary text-primary-content flex items-center justify-center font-bold text-xs shadow-2xs"
      >
        <img
          v-if="showImage"
          :src="auth.user!.avatar_url"
          :alt="auth.user!.name"
          class="h-full w-full object-cover"
          data-testid="avatar-image"
          @error="onImgError"
        />
        <span v-else data-testid="avatar-initial">{{ initial }}</span>
      </span>
      <span
        v-if="auth.user?.name"
        class="hidden sm:inline max-w-28 truncate text-xs font-medium text-base-content/90"
      >
        {{ auth.user.name }}
      </span>
      <Icon
        v-if="auth.user?.name"
        icon="lucide:chevron-down"
        class="hidden sm:inline size-3.5 text-base-content/50 transition-transform duration-150"
        :class="{ 'rotate-180': open }"
        aria-hidden="true"
      />
    </button>
    <div
      v-if="open"
      class="menu absolute right-0 z-30 mt-2 w-60 rounded-xl border border-base-300/80 bg-base-100/95 backdrop-blur-md p-2 shadow-lg"
      role="menu"
      data-testid="avatar-menu"
    >
      <div v-if="auth.user" class="flex items-center gap-2.5 px-3 py-2" data-testid="avatar-header">
        <div
          class="size-8 shrink-0 overflow-hidden rounded-full bg-primary text-primary-content flex items-center justify-center font-bold text-xs"
        >
          <img
            v-if="showImage"
            :src="auth.user.avatar_url"
            :alt="auth.user.name"
            class="h-full w-full object-cover"
          />
          <span v-else>{{ initial }}</span>
        </div>
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-semibold" data-testid="avatar-name">
            {{ auth.user.name }}
          </p>
          <p v-if="auth.user.email" class="truncate text-xs opacity-70">{{ auth.user.email }}</p>
        </div>
      </div>
      <button
        v-if="auth.loginRequired"
        type="button"
        role="menuitem"
        class="btn btn-ghost btn-sm justify-start"
        data-testid="logout"
        @click="signOut"
      >
        <Icon icon="lucide:log-out" aria-hidden="true" /> {{ t('layout.signOut') }}
      </button>
      <p v-else class="px-3 py-1 text-xs opacity-70">{{ t('layout.singleUser') }}</p>
    </div>
  </div>
</template>
