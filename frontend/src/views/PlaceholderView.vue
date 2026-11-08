<script setup lang="ts">
import { useRoute } from 'vue-router'

const route = useRoute()
</script>

<template>
  <section class="mx-auto max-w-2xl p-4">
    <h1 class="text-2xl font-bold">{{ route.meta.title }}</h1>
    <p class="mt-2 opacity-70">Coming soon.</p>
  </section>
</template>
