<script setup lang="ts">
import { computed } from 'vue'
import { Icon } from '@iconify/vue'
import { useOfflineStore } from '../stores/offline'
import { useI18n } from '../i18n'

const { t } = useI18n()

const offline = useOfflineStore()
const pauseText = computed(() => {
  if (offline.paused === 'unauthorized') return t('offline.signInAgain')
  if (offline.paused === 'reauth_required') return t('offline.confirmIdentity')
  return ''
})
</script>

<template>
  <div v-if="offline.visible" class="space-y-1 px-4 pt-2" data-testid="offline-banner">
    <div
      v-if="!offline.online"
      role="status"
      class="alert alert-warning py-2 text-sm"
      data-testid="offline-state"
    >
      <Icon icon="lucide:cloud-off" aria-hidden="true" /> {{ t('offline.offline') }}
    </div>
    <div
      v-if="offline.pendingCount > 0"
      role="status"
      class="alert alert-info flex-wrap py-2 text-sm"
      data-testid="pending-state"
    >
      <span class="badge badge-neutral" data-testid="pending-count">{{
        offline.pendingCount
      }}</span>
      <span>{{ t('offline.pending') }}</span>
      <span v-if="pauseText" data-testid="pause-text">{{ pauseText }}</span>
      <button
        type="button"
        class="btn btn-sm"
        :disabled="offline.replaying"
        data-testid="retry-sync"
        @click="offline.retry()"
      >
        {{ t('common.retry') }}
      </button>
    </div>
    <div
      v-for="f in offline.failures"
      :key="f.key"
      role="alert"
      class="alert alert-error py-2 text-sm"
      data-testid="sync-failure"
    >
      <span>{{ t('offline.failure', { message: f.message }) }}</span>
      <button type="button" class="btn btn-sm" @click="offline.dismissFailure(f.key)">
        {{ t('common.dismiss') }}
      </button>
    </div>
  </div>
</template>
