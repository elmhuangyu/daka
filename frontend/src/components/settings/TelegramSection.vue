<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Icon } from '@iconify/vue'
import {
  ApiError,
  createTelegramLinkCode,
  describeError,
  getTelegram,
  patchTelegram,
  unlinkTelegram,
  type TelegramStatus,
} from '../../api'
import { useI18n } from '../../i18n'

const { t, tParts, locale } = useI18n()

const st = ref<TelegramStatus | null>(null)
const error = ref('')
const unavailable = ref(false)
const link = ref<{ code: string; deep_link?: string; expires_at: string } | null>(null)
const digest = ref('08:00')

function apply(s: TelegramStatus) {
  st.value = s
  digest.value = s.digest_time
  if (s.linked) link.value = null
}

onMounted(async () => {
  try {
    apply(await getTelegram())
  } catch (e) {
    error.value = describeError(e)
  }
})
// The one-time code is dropped when the section goes away.
onBeforeUnmount(() => (link.value = null))

async function connect() {
  error.value = ''
  try {
    link.value = await createTelegramLinkCode()
  } catch (e) {
    if (e instanceof ApiError && e.status === 503) unavailable.value = true
    else error.value = describeError(e)
  }
}

async function refresh() {
  try {
    apply(await getTelegram())
  } catch (e) {
    error.value = describeError(e)
  }
}

async function unlink() {
  error.value = ''
  try {
    await unlinkTelegram()
    link.value = null
    await refresh()
  } catch (e) {
    error.value = describeError(e)
  }
}

async function patch(body: {
  enabled?: boolean
  remind_sensitive?: boolean
  digest_time?: string
}) {
  error.value = ''
  try {
    apply(await patchTelegram(body))
  } catch (e) {
    error.value = describeError(e)
    await refresh()
  }
}
</script>

<template>
  <section
    class="card bg-base-200/50 border border-base-300 shadow-sm"
    data-testid="section-telegram"
  >
    <div class="card-body p-6 gap-6">
      <div class="flex items-center gap-3 border-b border-base-300/80 pb-4">
        <div class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Icon icon="lucide:send" class="size-5" aria-hidden="true" />
        </div>
        <div>
          <h2 class="text-lg font-bold leading-none">{{ t('settings.telegram.title') }}</h2>
        </div>
      </div>

      <div
        v-if="error"
        class="alert alert-error text-sm py-2.5 px-4 shadow-sm"
        role="alert"
        data-testid="tg-error"
      >
        <span>{{ error }}</span>
      </div>

      <div
        v-if="unavailable || (st && !st.configured)"
        class="rounded-xl border border-dashed border-base-300 p-8 text-center flex flex-col items-center justify-center gap-2"
        data-testid="tg-unconfigured"
      >
        <Icon icon="lucide:send" class="size-8 opacity-25" aria-hidden="true" />
        <p class="text-sm opacity-70 max-w-sm">{{ t('settings.telegram.unconfigured') }}</p>
      </div>

      <template v-else-if="st">
        <template v-if="st.linked">
          <div
            class="flex items-center justify-between rounded-xl bg-success/10 border border-success/20 p-4"
          >
            <div class="flex items-center gap-3">
              <div class="size-2 rounded-full bg-success"></div>
              <p class="text-sm font-medium" data-testid="tg-linked">
                {{
                  st.linked_at
                    ? t('settings.telegram.linkedSince', { date: st.linked_at.slice(0, 10) })
                    : t('settings.telegram.linked')
                }}
              </p>
            </div>
            <button
              class="btn btn-xs sm:btn-sm btn-ghost text-error hover:bg-error/10"
              data-testid="tg-unlink"
              @click="unlink"
            >
              {{ t('settings.telegram.unlink') }}
            </button>
          </div>

          <div class="space-y-3">
            <label
              class="rounded-xl border border-base-300 bg-base-100/50 p-4 flex items-center justify-between gap-4 cursor-pointer hover:bg-base-100/80 transition-colors"
            >
              <span class="text-sm font-medium">{{ t('settings.telegram.enabled') }}</span>
              <input
                type="checkbox"
                class="checkbox checkbox-primary checkbox-sm"
                :checked="st.enabled"
                data-testid="tg-enabled"
                @change="patch({ enabled: ($event.target as HTMLInputElement).checked })"
              />
            </label>

            <label
              class="rounded-xl border border-base-300 bg-base-100/50 p-4 flex items-center justify-between gap-4 cursor-pointer hover:bg-base-100/80 transition-colors"
            >
              <span class="text-sm font-medium">{{ t('settings.telegram.remindSensitive') }}</span>
              <input
                type="checkbox"
                class="checkbox checkbox-primary checkbox-sm"
                :checked="st.remind_sensitive"
                data-testid="tg-sensitive"
                @change="patch({ remind_sensitive: ($event.target as HTMLInputElement).checked })"
              />
            </label>
          </div>

          <form
            class="rounded-xl border border-base-300 bg-base-100/40 p-4 flex flex-col sm:flex-row sm:items-end gap-3"
            @submit.prevent="patch({ digest_time: digest })"
          >
            <label class="form-control gap-1 flex-1">
              <span class="label-text text-xs font-medium">{{
                t('settings.telegram.digestTime')
              }}</span>
              <input
                v-model="digest"
                type="time"
                class="input input-bordered input-sm max-w-44"
                data-testid="tg-digest"
              />
            </label>
            <button
              class="btn btn-sm btn-primary gap-1 sm:self-end"
              type="submit"
              data-testid="tg-digest-save"
            >
              <Icon icon="lucide:check" class="size-3.5" aria-hidden="true" />
              {{ t('settings.telegram.save') }}
            </button>
          </form>
        </template>

        <template v-else>
          <div class="rounded-xl border border-base-300 bg-base-100/40 p-5 space-y-4">
            <p class="text-sm opacity-80">{{ t('settings.telegram.notLinked') }}</p>
            <button
              v-if="!link"
              class="btn btn-sm btn-primary gap-1.5"
              data-testid="tg-connect"
              @click="connect"
            >
              <Icon icon="lucide:send" class="size-3.5" aria-hidden="true" />
              {{ t('settings.telegram.connect') }}
            </button>
            <div v-else class="space-y-4" data-testid="tg-code-box">
              <div
                class="rounded-lg bg-base-200/80 p-4 border border-base-300 text-sm leading-relaxed"
              >
                <template v-for="(p, i) in tParts('settings.telegram.sendCode')" :key="i">
                  <code
                    v-if="'slot' in p && p.slot === 'code'"
                    class="font-mono font-bold bg-base-300 px-2 py-0.5 rounded text-primary"
                    data-testid="tg-code"
                    >/start {{ link.code }}</code
                  >
                  <template v-else-if="'slot' in p && p.slot === 'bot'">{{
                    st.bot_username ? `(@${st.bot_username})` : ''
                  }}</template>
                  <template v-else-if="'slot' in p">{{
                    new Date(link.expires_at).toLocaleTimeString(locale)
                  }}</template>
                  <template v-else>{{ p.text }}</template>
                </template>
              </div>
              <div class="flex items-center gap-3">
                <a
                  v-if="link.deep_link"
                  :href="link.deep_link"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="btn btn-sm btn-primary gap-1.5"
                  data-testid="tg-deeplink"
                >
                  <Icon icon="lucide:send" class="size-3.5" aria-hidden="true" />
                  {{ t('settings.telegram.openTelegram') }}
                </a>
                <button class="btn btn-sm btn-outline" data-testid="tg-refresh" @click="refresh">
                  {{ t('settings.telegram.sent') }}
                </button>
              </div>
            </div>
          </div>
        </template>
      </template>
    </div>
  </section>
</template>
