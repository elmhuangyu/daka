<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Icon } from '@iconify/vue'
import {
  createToken,
  deleteToken,
  describeError,
  listCategories,
  listTokens,
  revokeToken,
  type CategoryDto,
  type TokenDto,
} from '../../api'
import { useI18n } from '../../i18n'
import { useSettingsStore } from '../../stores/settings'

const { t } = useI18n()
const settingsStore = useSettingsStore()

const tokens = ref<TokenDto[]>([])
const categories = ref<CategoryDto[]>([])
const error = ref('')
const name = ref('')
const expiry = ref<string>('90')
const selectedCategory = ref<string>('')
const busy = ref(false)

// The plaintext lives only in this ref while the dialog is open.
const created = ref<{ name: string; feed_path: string } | null>(null)
const copied = ref(false)

const feedUrl = computed(() =>
  created.value ? `${window.location.origin}${created.value.feed_path}` : '',
)

async function load() {
  try {
    const [tks, cats] = await Promise.allSettled([listTokens(), listCategories()])
    if (tks.status === 'fulfilled') {
      tokens.value = tks.value
    } else {
      error.value = describeError(tks.reason)
    }
    if (cats.status === 'fulfilled') {
      categories.value = cats.value
    }
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
    const payload: Parameters<typeof createToken>[0] = {
      name: name.value.trim(),
      category_id: selectedCategory.value ? Number(selectedCategory.value) : null,
    }
    if (expiry.value === 'never') {
      payload.never_expires = true
    } else {
      payload.expires_in_days = Number(expiry.value)
    }
    const tk = await createToken(payload)
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

async function remove(tk: TokenDto) {
  error.value = ''
  try {
    await deleteToken(tk.id)
    await load()
  } catch (e) {
    error.value = describeError(e)
  }
}

const status = (tk: TokenDto) => (tk.revoked_at ? 'revoked' : tk.expired ? 'expired' : 'active')
const categoryText = (catId?: number | null) => {
  if (!catId) return t('settings.tokens.allCategories')
  const found = categories.value.find((c) => c.id === catId)
  return found ? settingsStore.categoryLabel(found) : t('settings.tokens.unknownCategory')
}
const expiresText = (tk: TokenDto) =>
  tk.expires_at
    ? t('settings.tokens.expiresAt', { date: tk.expires_at.slice(0, 10) })
    : t('settings.tokens.neverExpires')
const usedText = (tk: TokenDto) =>
  tk.last_used_at
    ? t('settings.tokens.lastUsedAt', { date: tk.last_used_at.slice(0, 10) })
    : t('settings.tokens.neverUsed')
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
                  category: categoryText(tk.category_id),
                  expires: expiresText(tk),
                  used: usedText(tk),
                })
              }}
            </span>
          </div>
          <div class="flex items-center gap-2">
            <button
              v-if="status(tk) === 'active'"
              class="btn btn-sm btn-ghost text-error hover:bg-error/10 text-xs"
              data-testid="tok-revoke"
              @click="revoke(tk)"
            >
              {{ t('settings.tokens.revoke') }}
            </button>
            <button
              v-else
              class="btn btn-sm btn-ghost text-error hover:bg-error/10 text-xs gap-1.5"
              data-testid="tok-delete"
              @click="remove(tk)"
            >
              <Icon icon="lucide:trash-2" class="size-3.5" aria-hidden="true" />
              {{ t('settings.tokens.delete') }}
            </button>
          </div>
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
            <span class="label-text text-xs font-medium">{{ t('settings.tokens.category') }}</span>
            <select
              v-model="selectedCategory"
              class="select select-bordered select-sm w-full"
              data-testid="tok-category"
            >
              <option value="">{{ t('settings.tokens.allCategories') }}</option>
              <option v-for="c in categories" :key="c.id" :value="String(c.id)">
                {{ settingsStore.categoryLabel(c) }}
              </option>
            </select>
          </label>
          <label class="form-control gap-1">
            <span class="label-text text-xs font-medium">{{ t('settings.tokens.expiresIn') }}</span>
            <select
              v-model="expiry"
              class="select select-bordered select-sm w-full"
              data-testid="tok-expiry"
            >
              <option value="30">{{ t('settings.tokens.days30') }}</option>
              <option value="90">{{ t('settings.tokens.days90') }}</option>
              <option value="180">{{ t('settings.tokens.days180') }}</option>
              <option value="365">{{ t('settings.tokens.days365') }}</option>
              <option value="never">{{ t('settings.tokens.neverExpires') }}</option>
            </select>
          </label>
        </div>
        <div class="flex items-center justify-end gap-3 pt-1 border-t border-base-300/50">
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
