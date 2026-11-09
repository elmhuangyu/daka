<script setup lang="ts">
import { ref } from 'vue'
import { Icon } from '@iconify/vue'
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
  <details
    class="card card-border bg-base-100 border-base-300 shadow-xs mt-6 transition-all duration-200 group"
  >
    <summary
      class="p-4 cursor-pointer flex items-center justify-between font-semibold select-none list-none text-base hover:text-primary transition-colors"
    >
      <span class="flex items-center gap-2">
        <Icon icon="lucide:plus" class="size-4 text-primary" aria-hidden="true" />
        {{ t('quickAdd.title') }}
      </span>
      <Icon
        icon="lucide:chevron-down"
        class="size-4 opacity-60 transition-transform duration-200 group-open:rotate-180"
        aria-hidden="true"
      />
    </summary>
    <div class="px-4 pb-4 pt-1 border-t border-base-300/60">
      <form class="flex flex-wrap items-center gap-2.5 pt-2" @submit.prevent="submit">
        <input
          v-model="name"
          class="input input-sm input-bordered flex-1 min-w-40"
          :aria-label="t('quickAdd.name')"
          :placeholder="t('quickAdd.name')"
        />
        <select
          v-model="presetKind"
          class="select select-sm select-bordered"
          :aria-label="t('quickAdd.repeat')"
        >
          <option value="days">{{ t('quickAdd.everyN') }}</option>
          <option value="weekly">{{ t('quickAdd.weeklyOn') }}</option>
        </select>
        <input
          v-if="presetKind === 'days'"
          v-model.number="interval"
          type="number"
          class="input input-sm input-bordered w-20"
          min="1"
          :aria-label="t('quickAdd.interval')"
        />
        <fieldset v-else class="flex flex-wrap items-center gap-2">
          <label
            v-for="(d, i) in WEEKDAYS"
            :key="d"
            class="flex items-center gap-1 text-xs cursor-pointer select-none"
          >
            <input v-model="days" type="checkbox" :value="d" class="checkbox checkbox-xs" />
            {{ weekdayName((i + 1) % 7, 'short') }}
          </label>
        </fieldset>
        <button type="submit" class="btn btn-sm btn-primary shadow-xs">
          {{ t('quickAdd.create') }}
        </button>
      </form>
      <p
        v-if="error"
        class="alert alert-error text-xs py-2 mt-3"
        role="alert"
        data-testid="quick-add-error"
      >
        {{ error }}
      </p>
    </div>
  </details>
</template>
