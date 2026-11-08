<script setup lang="ts">
import { ref } from 'vue'
import { createItem, describeError } from '../../api'
import { buildRRule, WEEKDAYS, type Weekday } from '../../rrule'

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
    error.value = 'Name is required'
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
    <summary class="cursor-pointer text-lg font-semibold">New item</summary>
    <form class="mt-2 flex flex-wrap items-center gap-2" @submit.prevent="submit">
      <input v-model="name" class="input input-sm input-bordered" aria-label="Name" placeholder="Name" />
      <select v-model="presetKind" class="select select-sm select-bordered" aria-label="Repeat">
        <option value="days">Every N days</option>
        <option value="weekly">Weekly on</option>
      </select>
      <input
        v-if="presetKind === 'days'" v-model.number="interval" type="number" class="input input-sm input-bordered w-20"
        min="1" aria-label="Interval in days"
      />
      <fieldset v-else>
        <label v-for="d in WEEKDAYS" :key="d"><input v-model="days" type="checkbox" :value="d" /> {{ d }}</label>
      </fieldset>
      <button type="submit" class="btn btn-sm btn-primary">Create</button>
    </form>
    <p v-if="error" class="error my-2" role="alert" data-testid="quick-add-error">{{ error }}</p>
  </details>
</template>
