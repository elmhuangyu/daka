<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { Icon } from '@iconify/vue'
import type { Project } from '../api'
import { useI18n } from '../i18n'

const { t } = useI18n()

defineProps<{
  itemId: number
  project: Project | null
}>()
</script>

<template>
  <template v-if="project">
    <RouterLink
      :to="{ path: `/projects/${project.id}`, hash: `#task-${itemId}` }"
      class="badge badge-outline gap-1 hover:badge-primary"
      :title="t('today.card.openProject')"
      :style="project.color ? { borderColor: project.color } : {}"
      data-testid="project-badge"
    >
      <span v-if="project.emoji" aria-hidden="true">{{ project.emoji }}</span>
      <Icon v-else icon="lucide:folder" class="size-3" aria-hidden="true" />
      {{ project.name }}
    </RouterLink>
  </template>
</template>
