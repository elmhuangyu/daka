<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Icon } from '@iconify/vue'
import { describeError } from '../../api'
import { useAuthStore } from '../../stores/auth'
import { usePrivacyStore } from '../../stores/privacy'
import { useSettingsStore } from '../../stores/settings'
import { useI18n } from '../../i18n'

const { t, tParts } = useI18n()

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
  <div class="space-y-6">
    <section
      class="card bg-base-200/50 border border-base-300 shadow-sm"
      data-testid="section-privacy"
    >
      <div class="card-body p-6 gap-6">
        <div class="flex items-center gap-3 border-b border-base-300/80 pb-4">
          <div
            class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary"
          >
            <Icon icon="lucide:shield" class="size-5" aria-hidden="true" />
          </div>
          <div>
            <h2 class="text-lg font-bold leading-none">{{ t('settings.privacy.title') }}</h2>
          </div>
        </div>

        <div
          class="rounded-xl bg-base-100/50 border border-base-300 p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3"
        >
          <div class="flex flex-col gap-0.5">
            <span class="text-sm font-medium">{{ t('settings.privacy.title') }}</span>
            <p class="text-xs opacity-70">
              <template v-for="(p, i) in tParts('settings.privacy.guardStatus')" :key="i">
                <strong v-if="'slot' in p" class="text-primary">{{
                  privacy.redact ? t('settings.privacy.on') : t('settings.privacy.off')
                }}</strong>
                <template v-else>{{ p.text }}</template>
              </template>
            </p>
          </div>
          <span class="badge badge-sm" :class="privacy.redact ? 'badge-primary' : 'badge-ghost'">
            {{ privacy.redact ? t('settings.privacy.on') : t('settings.privacy.off') }}
          </span>
        </div>

        <label
          class="rounded-xl border border-base-300 bg-base-100/50 p-4 flex items-center justify-between gap-4 cursor-pointer hover:bg-base-100/80 transition-colors"
        >
          <div class="flex flex-col gap-0.5">
            <span class="text-sm font-medium">{{ t('settings.privacy.mask') }}</span>
          </div>
          <input
            type="checkbox"
            class="checkbox checkbox-primary checkbox-sm"
            :checked="settings.maskCategories"
            data-testid="p-mask"
            @change="settings.setMaskCategories(($event.target as HTMLInputElement).checked)"
          />
        </label>
      </div>
    </section>

    <section
      class="card bg-base-200/50 border border-base-300 shadow-sm"
      data-testid="section-account"
    >
      <div class="card-body p-6 gap-6">
        <div class="flex items-center gap-3 border-b border-base-300/80 pb-4">
          <div
            class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary"
          >
            <Icon icon="lucide:user" class="size-5" aria-hidden="true" />
          </div>
          <div>
            <h2 class="text-lg font-bold leading-none">{{ t('settings.account.title') }}</h2>
          </div>
        </div>

        <div
          v-if="auth.user"
          class="rounded-xl border border-base-300 bg-base-100/60 p-4 flex items-center gap-4"
        >
          <div
            class="size-12 shrink-0 overflow-hidden rounded-full bg-primary/20 text-primary flex items-center justify-center font-bold text-base ring-2 ring-primary/30"
          >
            <img
              v-if="showAvatar"
              :src="auth.user.avatar_url"
              :alt="auth.user.name"
              class="h-full w-full object-cover"
              data-testid="account-avatar-image"
              @error="imgFailed = true"
            />
            <span v-else data-testid="account-avatar-initial">{{
              (auth.user.name ?? '?').trim().charAt(0).toUpperCase() || '?'
            }}</span>
          </div>
          <div class="min-w-0 flex-1">
            <p class="font-semibold text-sm truncate">{{ auth.user.name }}</p>
            <p v-if="auth.user.email" class="text-xs opacity-70 truncate">{{ auth.user.email }}</p>
          </div>
        </div>

        <div
          v-if="!auth.loginRequired"
          class="rounded-xl bg-base-100/50 border border-base-300 p-4 text-xs leading-relaxed opacity-80"
          data-testid="claim-hint"
        >
          {{ t('settings.account.noLogin') }}
        </div>

        <div v-if="error" class="alert alert-error text-sm py-2.5 px-4 shadow-sm" role="alert">
          <span>{{ error }}</span>
        </div>

        <div v-if="auth.loginRequired" class="pt-2 border-t border-base-300/80">
          <button
            class="btn btn-sm btn-outline text-error hover:bg-error/10 gap-1.5"
            data-testid="logout"
            @click="logout"
          >
            <Icon icon="lucide:log-out" class="size-3.5" aria-hidden="true" />
            {{ t('settings.account.logout') }}
          </button>
        </div>
      </div>
    </section>
  </div>
</template>
