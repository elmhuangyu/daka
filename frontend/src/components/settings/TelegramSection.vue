<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import {
  ApiError, createTelegramLinkCode, describeError, getTelegram, patchTelegram, unlinkTelegram, type TelegramStatus,
} from '../../api'

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

async function patch(body: { enabled?: boolean; remind_sensitive?: boolean; digest_time?: string }) {
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
      <h2 class="card-title">Telegram</h2>
      <p v-if="error" class="text-error" role="alert" data-testid="tg-error">{{ error }}</p>
      <p v-if="unavailable || (st && !st.configured)" data-testid="tg-unconfigured">
        The Telegram bot is not configured on this server.
      </p>
      <template v-else-if="st">
        <template v-if="st.linked">
          <p data-testid="tg-linked">Linked{{ st.linked_at ? ` since ${st.linked_at.slice(0, 10)}` : '' }}.</p>
          <label class="label cursor-pointer justify-start gap-3">
            <input
              type="checkbox" class="checkbox" :checked="st.enabled" data-testid="tg-enabled"
              @change="patch({ enabled: ($event.target as HTMLInputElement).checked })"
            />
            <span class="label-text">Send reminders and the daily digest</span>
          </label>
          <label class="label cursor-pointer justify-start gap-3">
            <input
              type="checkbox" class="checkbox" :checked="st.remind_sensitive" data-testid="tg-sensitive"
              @change="patch({ remind_sensitive: ($event.target as HTMLInputElement).checked })"
            />
            <span class="label-text">Remind sensitive items (otherwise they are sent as "a private item" or left out)</span>
          </label>
          <form class="flex items-end gap-2" @submit.prevent="patch({ digest_time: digest })">
            <label class="form-control">
              <span class="label-text">Daily digest time (empty disables)</span>
              <input v-model="digest" type="time" class="input input-bordered input-sm" data-testid="tg-digest" />
            </label>
            <button class="btn btn-sm btn-primary" type="submit" data-testid="tg-digest-save">Save</button>
          </form>
          <button class="btn btn-sm btn-ghost text-error self-start" data-testid="tg-unlink" @click="unlink">Unlink</button>
        </template>
        <template v-else>
          <p>Not linked.</p>
          <button v-if="!link" class="btn btn-sm btn-primary self-start" data-testid="tg-connect" @click="connect">Connect Telegram</button>
          <div v-else class="flex flex-col gap-2" data-testid="tg-code-box">
            <p>
              Send <code data-testid="tg-code">/start {{ link.code }}</code> to the bot
              <template v-if="st.bot_username">(@{{ st.bot_username }})</template>. The code expires at
              {{ new Date(link.expires_at).toLocaleTimeString() }}.
            </p>
            <a v-if="link.deep_link" :href="link.deep_link" target="_blank" rel="noopener noreferrer" class="link" data-testid="tg-deeplink">
              Open Telegram
            </a>
            <button class="btn btn-sm btn-outline self-start" data-testid="tg-refresh" @click="refresh">I have sent it</button>
          </div>
        </template>
      </template>
    </div>
  </section>
</template>
