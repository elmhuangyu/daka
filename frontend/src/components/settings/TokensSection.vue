<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Icon } from '@iconify/vue'
import { createToken, describeError, listTokens, revokeToken, type TokenDto } from '../../api'

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
    const t = await createToken({
      name: name.value.trim(), privacy: privacy.value, expires_in_days: days.value, include_negative: includeNegative.value,
    })
    created.value = { name: t.name, feed_path: t.feed_path ?? '' }
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

async function revoke(t: TokenDto) {
  error.value = ''
  try {
    await revokeToken(t.id)
    await load()
  } catch (e) {
    error.value = describeError(e)
  }
}

const status = (t: TokenDto) => (t.revoked_at ? 'revoked' : t.expired ? 'expired' : 'active')
const day = (s: string | null) => (s ? s.slice(0, 10) : 'never')
</script>

<template>
  <section class="card bg-base-200" data-testid="section-tokens">
    <div class="card-body gap-3">
      <h2 class="card-title">Feed &amp; tokens</h2>
      <p class="text-sm opacity-80">
        A token gives read-only access to your calendar feed. Subscribe to the feed URL from a calendar app
        (ICS / webcal). The token is part of the URL, so make sure any reverse proxy or server logs redact the
        <code>token=</code> query parameter.
      </p>
      <p v-if="error" class="text-error" role="alert" data-testid="tok-error">{{ error }}</p>
      <ul class="flex flex-col gap-2">
        <li v-for="t in tokens" :key="t.id" class="flex flex-wrap items-center gap-2" data-testid="tok-row">
          <span class="font-medium">{{ t.name }}</span>
          <span class="badge" :class="status(t) === 'active' ? 'badge-success' : 'badge-ghost'">{{ status(t) }}</span>
          <span class="text-xs opacity-70">{{ t.privacy }} · expires {{ day(t.expires_at) }} · last used {{ day(t.last_used_at) }}</span>
          <span class="flex-1" />
          <button v-if="status(t) === 'active'" class="btn btn-sm btn-ghost text-error" data-testid="tok-revoke" @click="revoke(t)">
            Revoke
          </button>
        </li>
        <li v-if="!tokens.length" class="opacity-70">No tokens.</li>
      </ul>
      <form class="flex flex-wrap items-end gap-2" @submit.prevent="create">
        <label class="form-control">
          <span class="label-text">Name</span>
          <input v-model="name" class="input input-bordered input-sm" maxlength="64" data-testid="tok-name" />
        </label>
        <label class="form-control">
          <span class="label-text">Expires in (days)</span>
          <input v-model.number="days" type="number" min="1" max="365" class="input input-bordered input-sm w-28" data-testid="tok-days" />
        </label>
        <label class="form-control">
          <span class="label-text">Sensitive items</span>
          <select v-model="privacy" class="select select-bordered select-sm" data-testid="tok-privacy">
            <option value="exclude_sensitive">Excluded</option>
            <option value="anonymize_sensitive">Anonymized</option>
            <option value="all">Included</option>
          </select>
        </label>
        <label class="label cursor-pointer gap-2">
          <input v-model="includeNegative" type="checkbox" class="checkbox checkbox-sm" />
          <span class="label-text">Include negative habits</span>
        </label>
        <button class="btn btn-sm btn-primary" type="submit" :disabled="busy" data-testid="tok-create">Create token</button>
      </form>
    </div>

    <div v-if="created" class="modal modal-open" role="dialog" aria-modal="true" data-testid="tok-dialog">
      <div class="modal-box">
        <h3 class="text-lg font-bold">Token created: {{ created.name }}</h3>
        <p class="mt-2 text-warning" role="alert">
          Copy the feed URL now. It is shown only once and cannot be retrieved later.
        </p>
        <input class="input input-bordered mt-2 w-full font-mono text-xs" readonly :value="feedUrl" data-testid="tok-plain" />
        <p class="mt-2 text-sm opacity-80">
          Add this URL as a subscribed calendar (ICS). Redact <code>token=</code> in reverse-proxy logs.
        </p>
        <div class="modal-action">
          <button class="btn" data-testid="tok-copy" @click="copy">
            <Icon icon="lucide:copy" aria-hidden="true" />{{ copied ? 'Copied' : 'Copy' }}
          </button>
          <button class="btn btn-primary" data-testid="tok-close" @click="closeDialog">Done</button>
        </div>
      </div>
    </div>
  </section>
</template>
