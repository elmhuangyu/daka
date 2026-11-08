import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { usePrivacyStore } from './privacy'
import { t } from '../i18n'

export const MASK_STORAGE_KEY = 'daka.mask_sensitive_categories'

function load(): boolean {
  try {
    // Default on: only an explicit "0" turns masking off.
    return localStorage.getItem(MASK_STORAGE_KEY) !== '0'
  } catch {
    return true
  }
}

/**
 * Settings-page preferences kept in the browser only. Never put tokens or
 * other secrets here.
 */
export const useSettingsStore = defineStore('settings', () => {
  const privacy = usePrivacyStore()
  const maskCategories = ref(load())

  function setMaskCategories(on: boolean) {
    maskCategories.value = on
    try {
      localStorage.setItem(MASK_STORAGE_KEY, on ? '1' : '0')
    } catch {
      // storage unavailable; keep the in-memory value only
    }
  }

  /** True when sensitive category names must not be rendered right now. */
  const masking = computed(() => privacy.redact && maskCategories.value)

  function categoryLabel(c: { name: string; is_sensitive: boolean }): string {
    return masking.value && c.is_sensitive ? t('settings.categories.masked') : c.name
  }

  return { maskCategories, masking, setMaskCategories, categoryLabel }
})
