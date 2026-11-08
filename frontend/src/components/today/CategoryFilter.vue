<script setup lang="ts">
import type { Category } from '../../api'
import type { CategoryFilter } from '../../today/logic'

defineProps<{
  categories: Category[]
  modelValue: CategoryFilter
  total: number
  counts: Map<number | 'none', number>
}>()
defineEmits<{ 'update:modelValue': [CategoryFilter] }>()
</script>

<template>
  <div class="flex flex-wrap gap-2" role="group" aria-label="Category filter" data-testid="category-filter">
    <button
      type="button" class="btn btn-sm" :class="modelValue === 'all' ? 'btn-primary' : 'btn-outline'"
      :aria-pressed="modelValue === 'all'" data-testid="filter-all" @click="$emit('update:modelValue', 'all')"
    >All ({{ total }})</button>
    <button
      v-for="c in categories" :key="c.id" type="button" class="btn btn-sm"
      :class="modelValue === c.id ? 'btn-primary' : 'btn-outline'" :aria-pressed="modelValue === c.id"
      :data-testid="`filter-${c.id}`" @click="$emit('update:modelValue', c.id)"
    >{{ c.name }} ({{ counts.get(c.id) ?? 0 }})</button>
    <button
      v-if="counts.get('none')" type="button" class="btn btn-sm" :class="modelValue === 'none' ? 'btn-primary' : 'btn-outline'"
      :aria-pressed="modelValue === 'none'" data-testid="filter-none" @click="$emit('update:modelValue', 'none')"
    >Uncategorized ({{ counts.get('none') }})</button>
  </div>
</template>
