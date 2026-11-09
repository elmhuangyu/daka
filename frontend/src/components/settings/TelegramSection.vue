<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
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
  <section class="card bg-base-200" data-testid="section-telegram">
    <div class="card-body gap-3">
      <h2 class="card-title">{{ t('settings.telegram.title') }}</h2>
      <p v-if="error" class="text-error" role="alert" data-testid="tg-error">{{ error }}</p>
      <p v-if="unavailable || (st && !st.configured)" data-testid="tg-unconfigured">
        {{ t('settings.telegram.unconfigured') }}
      </p>
      <template v-else-if="st">
        <template v-if="st.linked">
          <p data-testid="tg-linked">
            {{
              st.linked_at
                ? t('settings.telegram.linkedSince', { date: st.linked_at.slice(0, 10) })
                : t('settings.telegram.linked')
            }}
          </p>
          <label class="label cursor-pointer justify-start gap-3">
            <input
              type="checkbox"
              class="checkbox"
              :checked="st.enabled"
              data-testid="tg-enabled"
              @change="patch({ enabled: ($event.target as HTMLInputElement).checked })"
            />
            <span class="label-text">{{ t('settings.telegram.enabled') }}</span>
          </label>
          <label class="label cursor-pointer justify-start gap-3">
            <input
              type="checkbox"
              class="checkbox"
              :checked="st.remind_sensitive"
              data-testid="tg-sensitive"
              @change="patch({ remind_sensitive: ($event.target as HTMLInputElement).checked })"
            />
            <span class="label-text">{{ t('settings.telegram.remindSensitive') }}</span>
          </label>
          <form class="flex items-end gap-2" @submit.prevent="patch({ digest_time: digest })">
            <label class="form-control">
              <span class="label-text">{{ t('settings.telegram.digestTime') }}</span>
              <input
                v-model="digest"
                type="time"
                class="input input-bordered input-sm"
                data-testid="tg-digest"
              />
            </label>
            <button class="btn btn-sm btn-primary" type="submit" data-testid="tg-digest-save">
              {{ t('settings.telegram.save') }}
            </button>
          </form>
          <button
            class="btn btn-sm btn-ghost text-error self-start"
            data-testid="tg-unlink"
            @click="unlink"
          >
            {{ t('settings.telegram.unlink') }}
          </button>
        </template>
        <template v-else>
          <p>{{ t('settings.telegram.notLinked') }}</p>
          <button
            v-if="!link"
            class="btn btn-sm btn-primary self-start"
            data-testid="tg-connect"
            @click="connect"
          >
            {{ t('settings.telegram.connect') }}
          </button>
          <div v-else class="flex flex-col gap-2" data-testid="tg-code-box">
            <p>
              <template v-for="(p, i) in tParts('settings.telegram.sendCode')" :key="i">
                <code v-if="'slot' in p && p.slot === 'code'" data-testid="tg-code"
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
            </p>
            <a
              v-if="link.deep_link"
              :href="link.deep_link"
              target="_blank"
              rel="noopener noreferrer"
              class="link"
              data-testid="tg-deeplink"
            >
              {{ t('settings.telegram.openTelegram') }}
            </a>
            <button
              class="btn btn-sm btn-outline self-start"
              data-testid="tg-refresh"
              @click="refresh"
            >
              {{ t('settings.telegram.sent') }}
            </button>
          </div>
        </template>
      </template>
    </div>
  </section>
</template>
