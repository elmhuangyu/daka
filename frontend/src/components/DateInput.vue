<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Icon } from '@iconify/vue'
import { useI18n } from '../i18n'
import {
  currentDateFormat,
  dateFormatPlaceholder,
  formatCustomDate,
  parseCustomDate,
} from '../i18n/format'

const { t } = useI18n()

const props = withDefaults(
  defineProps<{
    modelValue?: string
    placeholder?: string
    clearable?: boolean
    dataTestid?: string
    ariaLabel?: string
    min?: string
    max?: string
    disabled?: boolean
  }>(),
  {
    modelValue: '',
    placeholder: '',
    clearable: false,
    dataTestid: undefined,
    ariaLabel: undefined,
    min: undefined,
    max: undefined,
    disabled: false,
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const pickerRef = ref<HTMLInputElement | null>(null)
const inputRef = ref<HTMLInputElement | null>(null)

const activeFormat = computed(() => currentDateFormat())
const effectivePlaceholder = computed(
  () => props.placeholder || dateFormatPlaceholder(activeFormat.value),
)

const text = ref(props.modelValue ? formatCustomDate(props.modelValue, activeFormat.value) : '')

watch([() => props.modelValue, activeFormat], ([val, fmt]) => {
  text.value = val ? formatCustomDate(val, fmt) : ''
})

function onTextInput(e: Event) {
  const raw = (e.target as HTMLInputElement).value
  text.value = raw
  if (!raw.trim()) {
    if (props.clearable) {
      emit('update:modelValue', '')
    }
    return
  }
  const parsed = parseCustomDate(raw, activeFormat.value)
  if (parsed) {
    emit('update:modelValue', parsed)
  }
}

function onBlur() {
  const trimmed = text.value.trim()
  if (!trimmed) {
    if (props.clearable) {
      emit('update:modelValue', '')
    }
    text.value = props.clearable
      ? ''
      : props.modelValue
        ? formatCustomDate(props.modelValue, activeFormat.value)
        : ''
    return
  }
  const parsed = parseCustomDate(trimmed, activeFormat.value)
  if (parsed) {
    emit('update:modelValue', parsed)
    text.value = formatCustomDate(parsed, activeFormat.value)
  } else {
    // Revert to valid modelValue if entered text is invalid
    text.value = props.modelValue ? formatCustomDate(props.modelValue, activeFormat.value) : ''
  }
}

function onPickerChange(e: Event) {
  const val = (e.target as HTMLInputElement).value
  if (val) {
    emit('update:modelValue', val)
    text.value = formatCustomDate(val, activeFormat.value)
  }
}

function openPicker() {
  if (props.disabled) return
  if (pickerRef.value?.showPicker) {
    pickerRef.value.showPicker()
  } else {
    pickerRef.value?.focus()
  }
}

function clear() {
  emit('update:modelValue', '')
  text.value = ''
}
</script>

<template>
  <label
    class="input input-bordered input-sm flex items-center gap-1.5 focus-within:outline-none focus-within:border-primary relative w-40 sm:w-44"
    :class="{ 'opacity-60 cursor-not-allowed': disabled }"
  >
    <input
      ref="inputRef"
      type="text"
      class="grow font-mono text-xs sm:text-sm bg-transparent outline-none w-0 min-w-0"
      :value="text"
      :placeholder="effectivePlaceholder"
      :data-testid="dataTestid"
      :aria-label="ariaLabel"
      :disabled="disabled"
      @input="onTextInput"
      @blur="onBlur"
      @keydown.enter.prevent="onBlur"
    />
    <button
      v-if="clearable && modelValue"
      type="button"
      class="text-base-content/40 hover:text-base-content/80 cursor-pointer p-0.5 shrink-0"
      :aria-label="t('common.clear')"
      :title="t('common.clear')"
      tabindex="-1"
      @click.stop="clear"
    >
      <Icon icon="lucide:x" class="size-3.5" />
    </button>
    <button
      type="button"
      class="text-base-content/60 hover:text-primary cursor-pointer p-0.5 shrink-0"
      :aria-label="t('calendar.pickDate')"
      :title="t('calendar.pickDate')"
      tabindex="-1"
      @click.stop="openPicker"
    >
      <Icon icon="lucide:calendar" class="size-4" />
    </button>
    <input
      ref="pickerRef"
      type="date"
      class="sr-only"
      :value="modelValue"
      :min="min"
      :max="max"
      :disabled="disabled"
      tabindex="-1"
      @change="onPickerChange"
    />
  </label>
</template>
