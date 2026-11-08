<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import TodayView from './TodayView.vue'

const STORAGE_KEY = 'daka.redact'

function loadRedact(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    return false
  }
}

const redact = ref(loadRedact())

function toggleRedact() {
  redact.value = !redact.value
  try {
    localStorage.setItem(STORAGE_KEY, redact.value ? '1' : '0')
  } catch {
    // storage unavailable; keep the in-memory value only
  }
}

function onKey(e: KeyboardEvent) {
  if (!e.repeat && e.ctrlKey && e.shiftKey && e.key.toLowerCase() === 'h') {
    e.preventDefault()
    toggleRedact()
  }
}

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <main>
    <div class="row">
      <button type="button" data-testid="redact-toggle" :aria-pressed="redact" @click="toggleRedact">
        Privacy: {{ redact ? 'on' : 'off' }}
      </button>
    </div>
    <TodayView :redact="redact" />
  </main>
</template>
