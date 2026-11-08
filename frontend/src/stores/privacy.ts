import { defineStore } from 'pinia'
import { ref } from 'vue'

export const PRIVACY_STORAGE_KEY = 'daka.redact'

function load(): boolean {
  try {
    return localStorage.getItem(PRIVACY_STORAGE_KEY) === '1'
  } catch {
    return false
  }
}

function save(on: boolean) {
  try {
    localStorage.setItem(PRIVACY_STORAGE_KEY, on ? '1' : '0')
  } catch {
    // storage unavailable; keep the in-memory value only
  }
}

/**
 * Privacy guard (DESIGN 4.2). While `redact` is true every view must request
 * data with `?redact=1` and drop any plaintext it already holds. Views that
 * show data should watch `redact` and clear their state synchronously when it
 * turns on (see TodayView).
 */
export const usePrivacyStore = defineStore('privacy', () => {
  const redact = ref(load())

  function set(on: boolean) {
    redact.value = on
    save(on)
  }

  function toggle() {
    set(!redact.value)
  }

  return { redact, set, toggle }
})
