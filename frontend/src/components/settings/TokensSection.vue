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

const feedUrl = computed(() => (created.value ? `${window.location.origin}${created.value.feed_path}` : ''))

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
      name: name.value.trim(), privacy: privacy.value, expires_in_days: days.value, include_negative: includeNegative.value,
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
const privacyText = (p: string) => (p in PRIVACY_KEYS ? t(PRIVACY_KEYS[p as keyof typeof PRIVACY_KEYS]) : p)
</script>

<template>
  <section class="card bg-base-200" data-testid="section-tokens">
    <div class="card-body gap-3">
      <h2 class="card-title">{{ t('settings.tokens.title') }}</h2>
      <p class="text-sm opacity-80">{{ t('settings.tokens.help') }}</p>
      <p v-if="error" class="text-error" role="alert" data-testid="tok-error">{{ error }}</p>
      <ul class="flex flex-col gap-2">
        <li v-for="tk in tokens" :key="tk.id" class="flex flex-wrap items-center gap-2" data-testid="tok-row">
          <span class="font-medium">{{ tk.name }}</span>
          <span class="badge" :class="status(tk) === 'active' ? 'badge-success' : 'badge-ghost'">{{ t(`settings.tokens.${status(tk)}`) }}</span>
          <span class="text-xs opacity-70">{{ t('settings.tokens.meta', { privacy: privacyText(tk.privacy), expires: day(tk.expires_at), used: day(tk.last_used_at) }) }}</span>
          <span class="flex-1" />
          <button v-if="status(tk) === 'active'" class="btn btn-sm btn-ghost text-error" data-testid="tok-revoke" @click="revoke(tk)">
            {{ t('settings.tokens.revoke') }}
          </button>
        </li>
        <li v-if="!tokens.length" class="opacity-70">{{ t('settings.tokens.empty') }}</li>
      </ul>
      <form class="flex flex-wrap items-end gap-2" @submit.prevent="create">
        <label class="form-control">
          <span class="label-text">{{ t('settings.tokens.name') }}</span>
          <input v-model="name" class="input input-bordered input-sm" maxlength="64" data-testid="tok-name" />
        </label>
        <label class="form-control">
          <span class="label-text">{{ t('settings.tokens.expiresIn') }}</span>
          <input v-model.number="days" type="number" min="1" max="365" class="input input-bordered input-sm w-28" data-testid="tok-days" />
        </label>
        <label class="form-control">
          <span class="label-text">{{ t('settings.tokens.sensitiveItems') }}</span>
          <select v-model="privacy" class="select select-bordered select-sm" data-testid="tok-privacy">
            <option value="exclude_sensitive">{{ t('settings.tokens.excluded') }}</option>
            <option value="anonymize_sensitive">{{ t('settings.tokens.anonymized') }}</option>
            <option value="all">{{ t('settings.tokens.included') }}</option>
          </select>
        </label>
        <label class="label cursor-pointer gap-2">
          <input v-model="includeNegative" type="checkbox" class="checkbox checkbox-sm" />
          <span class="label-text">{{ t('settings.tokens.includeNegative') }}</span>
        </label>
        <button class="btn btn-sm btn-primary" type="submit" :disabled="busy" data-testid="tok-create">{{ t('settings.tokens.create') }}</button>
      </form>
    </div>

    <div v-if="created" class="modal modal-open" role="dialog" aria-modal="true" data-testid="tok-dialog">
      <div class="modal-box">
        <h3 class="text-lg font-bold">{{ t('settings.tokens.createdTitle', { name: created.name }) }}</h3>
        <p class="mt-2 text-warning" role="alert">
          {{ t('settings.tokens.copyNow') }}
        </p>
        <input class="input input-bordered mt-2 w-full font-mono text-xs" readonly :value="feedUrl" data-testid="tok-plain" />
        <p class="mt-2 text-sm opacity-80">{{ t('settings.tokens.addHelp') }}</p>
        <div class="modal-action">
          <button class="btn" data-testid="tok-copy" @click="copy">
            <Icon icon="lucide:copy" aria-hidden="true" />{{ copied ? t('settings.tokens.copied') : t('settings.tokens.copy') }}
          </button>
          <button class="btn btn-primary" data-testid="tok-close" @click="closeDialog">{{ t('settings.tokens.done') }}</button>
        </div>
      </div>
    </div>
  </section>
</template>
