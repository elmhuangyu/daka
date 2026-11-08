<script setup lang="ts">
import { Icon } from '@iconify/vue'
import { usePrivacyStore } from '../stores/privacy'

const privacy = usePrivacyStore()
</script>

<template>
  <button
    type="button"
    class="btn btn-sm"
    :class="privacy.redact ? 'btn-warning' : 'btn-ghost'"
    data-testid="redact-toggle"
    :aria-pressed="privacy.redact"
    title="Toggle privacy guard (Ctrl+Shift+H)"
    @click="privacy.toggle()"
  >
    <Icon :icon="privacy.redact ? 'lucide:eye-off' : 'lucide:eye'" aria-hidden="true" />
    <span>Privacy: {{ privacy.redact ? 'on' : 'off' }}</span>
  </button>
</template>
