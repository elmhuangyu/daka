<script setup lang="ts">
import { ref } from 'vue'
import { createItem, describeError } from '../../api'
import { buildRRule, WEEKDAYS, type Weekday } from '../../rrule'
import { useI18n } from '../../i18n'
import { weekdayName } from '../../i18n/format'

const { t } = useI18n()

// Temporary minimal creator; the Item Editor (P3-3) replaces it.
const emit = defineEmits<{ created: [] }>()

const name = ref('')
const presetKind = ref<'days' | 'weekly'>('days')
const interval = ref(1)
const days = ref<Weekday[]>([])
const error = ref('')

async function submit() {
  let rrule: string
  try {
    rrule =
      presetKind.value === 'days'
        ? buildRRule({ kind: 'days', interval: interval.value })
        : buildRRule({ kind: 'weekly', days: days.value })
  } catch (e) {
    error.value = (e as Error).message
    return
  }
  if (!name.value.trim()) {
    error.value = t('errors.nameRequired')
    return
  }
  try {
    await createItem({ name: name.value.trim(), rrule_string: rrule })
  } catch (e) {
    error.value = describeError(e)
    return
  }
  error.value = ''
  name.value = ''
  emit('created')
}
</script>

<template>
  <details class="mt-6">
    <summary class="cursor-pointer text-lg font-semibold">{{ t('quickAdd.title') }}</summary>
    <form class="mt-2 flex flex-wrap items-center gap-2" @submit.prevent="submit">
      <input v-model="name" class="input input-sm input-bordered" :aria-label="t('quickAdd.name')" :placeholder="t('quickAdd.name')" />
      <select v-model="presetKind" class="select select-sm select-bordered" :aria-label="t('quickAdd.repeat')">
        <option value="days">{{ t('quickAdd.everyN') }}</option>
        <option value="weekly">{{ t('quickAdd.weeklyOn') }}</option>
      </select>
      <input
        v-if="presetKind === 'days'" v-model.number="interval" type="number" class="input input-sm input-bordered w-20"
        min="1" :aria-label="t('quickAdd.interval')"
      />
      <fieldset v-else>
        <label v-for="(d, i) in WEEKDAYS" :key="d"><input v-model="days" type="checkbox" :value="d" /> {{ weekdayName((i + 1) % 7, 'short') }}</label>
      </fieldset>
      <button type="submit" class="btn btn-sm btn-primary">{{ t('quickAdd.create') }}</button>
    </form>
    <p v-if="error" class="error my-2" role="alert" data-testid="quick-add-error">{{ error }}</p>
  </details>
</template>
