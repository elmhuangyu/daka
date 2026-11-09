<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Icon } from '@iconify/vue'
import { createToken, describeError, listTokens, revokeToken, type TokenDto } from '../../api'
import { useI18n } from '../../i18n'

const { t } = useI18n()

const tokens = ref<TokenDto[]>([])
const error = ref('')
const name = ref('')
const days = ref(90)
const privacy = ref('exclude_sensitive')
const includeNegative = ref(false)
const busy = ref(false)

// The plaintext lives only in this ref while the dialog is open.
const created = ref<{ name: string; feed_path: string } | null>(null)
const copied = ref(false)

const feedUrl = computed(() =>
  created.value ? `${window.location.origin}${created.value.feed_path}` : '',
)

async function load() {
  try {
    tokens.value = await listTokens()
  } catch (e) {
    error.value = describeError(e)
  }
}
onMounted(load)

function closeDialog() {
  created.value = null
  copied.value = false
}
onBeforeUnmount(closeDialog)

async function create() {
  if (!name.value.trim()) return
  busy.value = true
  error.value = ''
  try {
    const tk = await createToken({
      name: name.value.trim(),
      privacy: privacy.value,
      expires_in_days: days.value,
      include_negative: includeNegative.value,
    })
    created.value = { name: tk.name, feed_path: tk.feed_path ?? '' }
    name.value = ''
    await load()
  } catch (e) {
    error.value = describeError(e)
  } finally {
    busy.value = false
  }
}

async function copy() {
  try {
    await navigator.clipboard.writeText(feedUrl.value)
    copied.value = true
  } catch {
    copied.value = false
  }
}

async function revoke(tk: TokenDto) {
  error.value = ''
  try {
    await revokeToken(tk.id)
    await load()
  } catch (e) {
    error.value = describeError(e)
  }
}

const status = (tk: TokenDto) => (tk.revoked_at ? 'revoked' : tk.expired ? 'expired' : 'active')
const day = (s: string | null) => (s ? s.slice(0, 10) : t('settings.tokens.never'))
const PRIVACY_KEYS = {
  exclude_sensitive: 'settings.tokens.excluded',
  anonymize_sensitive: 'settings.tokens.anonymized',
  all: 'settings.tokens.included',
} as const
const privacyText = (p: string) =>
  p in PRIVACY_KEYS ? t(PRIVACY_KEYS[p as keyof typeof PRIVACY_KEYS]) : p
</script>

<template>
  <section
    class="card bg-base-200/50 border border-base-300 shadow-sm"
    data-testid="section-tokens"
  >
    <div class="card-body p-6 gap-6">
      <div class="flex items-center gap-3 border-b border-base-300/80 pb-4">
        <div class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Icon icon="lucide:key" class="size-5" aria-hidden="true" />
        </div>
        <div>
          <h2 class="text-lg font-bold leading-none">{{ t('settings.tokens.title') }}</h2>
        </div>
      </div>

      <div
        class="rounded-xl bg-base-100/50 border border-base-300 p-3.5 text-xs leading-relaxed opacity-80 flex items-start gap-2.5"
      >
        <Icon icon="lucide:info" class="size-4 shrink-0 text-info mt-0.5" aria-hidden="true" />
        <span>{{ t('settings.tokens.help') }}</span>
      </div>

      <div
        v-if="error"
        class="alert alert-error text-sm py-2.5 px-4 shadow-sm"
        role="alert"
        data-testid="tok-error"
      >
        <span>{{ error }}</span>
      </div>

      <ul class="flex flex-col gap-2">
        <li
          v-for="tk in tokens"
          :key="tk.id"
          class="rounded-xl border border-base-300 bg-base-100/60 p-3.5 flex flex-wrap items-center justify-between gap-3 transition-colors hover:bg-base-100/90"
          data-testid="tok-row"
        >
          <div class="flex flex-col gap-1 min-w-0">
            <div class="flex items-center gap-2">
              <span class="font-semibold text-sm">{{ tk.name }}</span>
              <span
                class="badge badge-sm"
                :class="status(tk) === 'active' ? 'badge-success' : 'badge-ghost'"
              >
                {{ t(`settings.tokens.${status(tk)}`) }}
              </span>
            </div>
            <span class="text-xs opacity-60">
              {{
                t('settings.tokens.meta', {
                  privacy: privacyText(tk.privacy),
                  expires: day(tk.expires_at),
                  used: day(tk.last_used_at),
                })
              }}
            </span>
          </div>
          <button
            v-if="status(tk) === 'active'"
            class="btn btn-sm btn-ghost text-error hover:bg-error/10 text-xs"
            data-testid="tok-revoke"
            @click="revoke(tk)"
          >
            {{ t('settings.tokens.revoke') }}
          </button>
        </li>
        <li
          v-if="!tokens.length"
          class="rounded-xl border border-dashed border-base-300 p-8 text-center flex flex-col items-center justify-center gap-2"
        >
          <Icon icon="lucide:key" class="size-8 opacity-25" aria-hidden="true" />
          <span class="text-sm opacity-70">{{ t('settings.tokens.empty') }}</span>
        </li>
      </ul>

      <form
        class="rounded-xl border border-base-300 bg-base-100/40 p-4 space-y-4"
        @submit.prevent="create"
      >
        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          <label class="form-control gap-1">
            <span class="label-text text-xs font-medium">{{ t('settings.tokens.name') }}</span>
            <input
              v-model="name"
              class="input input-bordered input-sm w-full"
              maxlength="64"
              data-testid="tok-name"
              :placeholder="t('settings.tokens.name')"
            />
          </label>
          <label class="form-control gap-1">
            <span class="label-text text-xs font-medium">{{ t('settings.tokens.expiresIn') }}</span>
            <input
              v-model.number="days"
              type="number"
              min="1"
              max="365"
              class="input input-bordered input-sm w-full"
              data-testid="tok-days"
            />
          </label>
          <label class="form-control gap-1">
            <span class="label-text text-xs font-medium">{{
              t('settings.tokens.sensitiveItems')
            }}</span>
            <select
              v-model="privacy"
              class="select select-bordered select-sm w-full"
              data-testid="tok-privacy"
            >
              <option value="exclude_sensitive">{{ t('settings.tokens.excluded') }}</option>
              <option value="anonymize_sensitive">{{ t('settings.tokens.anonymized') }}</option>
              <option value="all">{{ t('settings.tokens.included') }}</option>
            </select>
          </label>
        </div>
        <div
          class="flex flex-wrap items-center justify-between gap-3 pt-1 border-t border-base-300/50"
        >
          <label class="label cursor-pointer gap-2 py-0">
            <input v-model="includeNegative" type="checkbox" class="checkbox checkbox-sm" />
            <span class="label-text text-xs">{{ t('settings.tokens.includeNegative') }}</span>
          </label>
          <button
            class="btn btn-sm btn-primary gap-1.5"
            type="submit"
            :disabled="busy"
            data-testid="tok-create"
          >
            <Icon icon="lucide:key" class="size-3.5" aria-hidden="true" />
            {{ t('settings.tokens.create') }}
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="created"
      class="modal modal-open"
      role="dialog"
      aria-modal="true"
      data-testid="tok-dialog"
    >
      <div class="modal-box">
        <h3 class="text-lg font-bold">
          {{ t('settings.tokens.createdTitle', { name: created.name }) }}
        </h3>
        <p class="mt-2 text-sm text-warning font-medium" role="alert">
          {{ t('settings.tokens.copyNow') }}
        </p>
        <input
          class="input input-bordered mt-3 w-full font-mono text-xs"
          readonly
          :value="feedUrl"
          data-testid="tok-plain"
        />
        <p class="mt-2 text-xs opacity-70">{{ t('settings.tokens.addHelp') }}</p>
        <div class="modal-action">
          <button class="btn btn-sm" data-testid="tok-copy" @click="copy">
            <Icon icon="lucide:copy" class="size-3.5" aria-hidden="true" />{{
              copied ? t('settings.tokens.copied') : t('settings.tokens.copy')
            }}
          </button>
          <button class="btn btn-sm btn-primary" data-testid="tok-close" @click="closeDialog">
            {{ t('settings.tokens.done') }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
