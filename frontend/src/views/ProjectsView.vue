<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Icon } from '@iconify/vue'
import EmojiPicker from '../components/EmojiPicker.vue'
import * as api from '../api'
import type { Category, CreateProjectRequest, Project, ProjectStatus } from '../api'
import DateInput from '../components/DateInput.vue'
import { catalogDef } from '../composables/shortcutCatalog'
import { useShortcut } from '../composables/useShortcuts'
import { useI18n } from '../i18n'
import { formatCustomDate } from '../i18n/format'
import { usePrivacyStore } from '../stores/privacy'
import { useProjectsStore } from '../stores/projects'

const { t } = useI18n()
const router = useRouter()
const privacy = usePrivacyStore()
const projectsStore = useProjectsStore()

const activeTab = ref<ProjectStatus>('active')
const categories = ref<Category[]>([])
const taskStats = ref<Map<number, { total: number; completed: number }>>(new Map())

const showNewModal = ref(false)
const modalError = ref('')
const saving = ref(false)

const COLOR_PRESETS = [
  { label: 'None', value: '' },
  { label: 'Emerald', value: '#10b981' },
  { label: 'Blue', value: '#3b82f6' },
  { label: 'Indigo', value: '#6366f1' },
  { label: 'Purple', value: '#8b5cf6' },
  { label: 'Pink', value: '#ec4899' },
  { label: 'Rose', value: '#f43f5e' },
  { label: 'Amber', value: '#f59e0b' },
  { label: 'Orange', value: '#f97316' },
  { label: 'Teal', value: '#14b8a6' },
  { label: 'Cyan', value: '#06b6d4' },
]

interface ProjectFormData {
  name: string
  description: string
  color: string
  emoji: string
  category_id: number | null
  target_date: string
  is_sensitive: boolean
}

const form = reactive<ProjectFormData>({
  name: '',
  description: '',
  color: '',
  emoji: '',
  category_id: null,
  target_date: '',
  is_sensitive: false,
})

const categoriesById = computed(() => {
  const map = new Map<number, Category>()
  for (const c of categories.value) {
    map.set(c.id, c)
  }
  return map
})

const currentProjects = computed<Project[]>(() => {
  switch (activeTab.value) {
    case 'active':
      return projectsStore.activeProjects
    case 'completed':
      return projectsStore.completedProjects
    case 'archived':
      return projectsStore.archivedProjects
    default:
      return []
  }
})

let seq = 0

async function loadCategories() {
  const my = ++seq
  try {
    const [itemsRes, catsRes] = await Promise.all([
      api.getItems(privacy.redact),
      api.getCategories(),
    ])
    if (my !== seq) return
    categories.value = (catsRes.categories ?? []).map((c) =>
      privacy.redact && c.is_sensitive ? { ...c, name: t('common.privateCategory'), icon: '' } : c,
    )
    const statsMap = new Map<number, { total: number; completed: number }>()
    for (const item of itemsRes.items) {
      if (item.project_id) {
        const s = statsMap.get(item.project_id) ?? { total: 0, completed: 0 }
        s.total++
        if ((item.stats?.counts?.total ?? 0) > 0) {
          s.completed++
        }
        statsMap.set(item.project_id, s)
      }
    }
    taskStats.value = statsMap
  } catch {
    // Categories and item stats are supplemental
  }
}

async function loadData() {
  await Promise.all([projectsStore.fetchProjects(), loadCategories()])
}

function onVisibilityChange() {
  if (document.visibilityState === 'visible') {
    void loadData()
  }
}

onMounted(() => {
  loadData()
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onUnmounted(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
})

watch(
  () => privacy.redact,
  (on) => {
    if (on) {
      categories.value = []
      seq++
    }
    void loadData()
  },
  { flush: 'sync' },
)

function openNewModal() {
  form.name = ''
  form.description = ''
  form.color = ''
  form.emoji = ''
  form.category_id = null
  form.target_date = ''
  form.is_sensitive = false
  modalError.value = ''
  showNewModal.value = true
  void nextTick(() =>
    document.querySelector<HTMLInputElement>('[data-testid="input-project-name"]')?.focus(),
  )
}

function closeNewModal() {
  showNewModal.value = false
  modalError.value = ''
  void nextTick(() =>
    document.querySelector<HTMLButtonElement>('[data-testid="btn-new-project"]')?.focus(),
  )
}

async function handleCreateProject() {
  const name = form.name.trim()
  if (!name) {
    modalError.value = t('projects.nameRequired')
    return
  }
  saving.value = true
  modalError.value = ''
  try {
    const req: CreateProjectRequest = {
      name,
      description: form.description.trim() || undefined,
      color: form.color || undefined,
      emoji: form.emoji || undefined,
      category_id: form.category_id,
      target_date: form.target_date || null,
      is_sensitive: form.is_sensitive,
    }
    await projectsStore.createProject(req)
    closeNewModal()
    await loadData()
  } catch (e) {
    modalError.value = api.describeError(e)
  } finally {
    saving.value = false
  }
}

function navigateToProject(id: number) {
  router.push(`/projects/${id}`)
}

async function updateProjectStatus(p: Project, newStatus: ProjectStatus) {
  try {
    await projectsStore.updateProject(p.id, { status: newStatus })
    if (privacy.redact) {
      await projectsStore.fetchProjects()
    }
  } catch (e) {
    projectsStore.error = api.describeError(e)
  }
}

function getProjectStats(projectId: number) {
  const s = taskStats.value.get(projectId) ?? { total: 0, completed: 0 }
  const percent = s.total > 0 ? Math.round((s.completed / s.total) * 100) : 0
  return { ...s, percent }
}

const gridEl = ref<HTMLElement | null>(null)

function moveProjectFocus(dir: 1 | -1): boolean {
  const cards = [...(gridEl.value?.querySelectorAll<HTMLElement>('[data-project-card]') ?? [])]
  if (!cards.length) return false
  const cur = document.activeElement?.closest<HTMLElement>('[data-project-card]') ?? null
  const i = cur ? cards.indexOf(cur) : -1
  const next =
    i < 0 ? (dir === 1 ? 0 : cards.length - 1) : Math.min(cards.length - 1, Math.max(0, i + dir))
  const el = cards[next]!
  if (el !== document.activeElement) {
    el.focus()
    el.scrollIntoView?.({ block: 'nearest' })
  }
  return true
}

useShortcut([
  catalogDef('project.rowUp', () => moveProjectFocus(-1)),
  catalogDef('project.rowDown', () => moveProjectFocus(1)),
])
</script>

<template>
  <div class="space-y-6 max-w-5xl mx-auto p-4 sm:p-6" data-testid="projects-view">
    <!-- Header -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-base-content flex items-center gap-2">
          <Icon icon="lucide:folder" class="size-7 text-primary" />
          <span>{{ t('projects.title') }}</span>
        </h1>
      </div>
      <button
        type="button"
        class="btn btn-primary gap-2 shadow-xs"
        data-testid="btn-new-project"
        @click="openNewModal"
      >
        <Icon icon="lucide:plus" class="size-4" />
        <span>{{ t('projects.new') }}</span>
      </button>
    </div>

    <!-- Error message if any -->
    <div
      v-if="projectsStore.error"
      class="alert alert-error shadow-sm text-sm"
      data-testid="projects-error"
    >
      <Icon icon="lucide:triangle-alert" class="size-5 shrink-0" />
      <span>{{ projectsStore.error }}</span>
    </div>

    <!-- Tabs -->
    <div class="tabs tabs-box bg-base-200/60 p-1 rounded-xl flex gap-1 w-fit" role="tablist">
      <button
        type="button"
        role="tab"
        class="tab rounded-lg font-medium transition-colors"
        :class="{ 'tab-active font-semibold shadow-xs': activeTab === 'active' }"
        data-testid="tab-active"
        @click="activeTab = 'active'"
      >
        <span>{{ t('projects.active') }}</span>
        <span class="badge badge-sm ml-1.5 opacity-80">{{
          projectsStore.activeProjects.length
        }}</span>
      </button>
      <button
        type="button"
        role="tab"
        class="tab rounded-lg font-medium transition-colors"
        :class="{ 'tab-active font-semibold shadow-xs': activeTab === 'completed' }"
        data-testid="tab-completed"
        @click="activeTab = 'completed'"
      >
        <span>{{ t('projects.completed') }}</span>
        <span class="badge badge-sm ml-1.5 opacity-80">{{
          projectsStore.completedProjects.length
        }}</span>
      </button>
      <button
        type="button"
        role="tab"
        class="tab rounded-lg font-medium transition-colors"
        :class="{ 'tab-active font-semibold shadow-xs': activeTab === 'archived' }"
        data-testid="tab-archived"
        @click="activeTab = 'archived'"
      >
        <span>{{ t('projects.archived') }}</span>
        <span class="badge badge-sm ml-1.5 opacity-80">{{
          projectsStore.archivedProjects.length
        }}</span>
      </button>
    </div>

    <!-- Loading State -->
    <div
      v-if="projectsStore.loading && currentProjects.length === 0"
      class="flex justify-center py-12"
    >
      <span class="loading loading-spinner loading-md text-primary" />
    </div>

    <!-- Empty State -->
    <div
      v-else-if="currentProjects.length === 0"
      class="rounded-2xl border border-dashed border-base-300 p-12 text-center"
      data-testid="projects-empty"
    >
      <div
        class="mx-auto flex size-12 items-center justify-center rounded-xl bg-base-200 text-base-content/60 mb-3"
      >
        <Icon icon="lucide:folder" class="size-6" />
      </div>
      <p class="text-sm font-medium text-base-content/80">{{ t('projects.noProjects') }}</p>
    </div>

    <!-- Projects Grid -->
    <div
      v-else
      ref="gridEl"
      class="grid grid-cols-1 md:grid-cols-2 gap-4"
      data-testid="projects-grid"
    >
      <div
        v-for="p in currentProjects"
        :key="p.id"
        tabindex="0"
        data-project-card
        class="card bg-base-100 border border-base-300 hover:border-primary/40 hover:shadow-md transition-all cursor-pointer overflow-hidden flex flex-col justify-between focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        :style="p.color ? { borderLeftColor: p.color, borderLeftWidth: '4px' } : {}"
        :data-testid="`project-card-${p.id}`"
        @click="navigateToProject(p.id)"
        @keydown.enter.self.prevent="navigateToProject(p.id)"
        @keydown.space.self.prevent="navigateToProject(p.id)"
      >
        <div class="card-body p-5 space-y-3">
          <!-- Card Title & Status Badge -->
          <div class="flex items-start justify-between gap-2">
            <h2 class="card-title text-lg font-semibold text-base-content flex items-center gap-2">
              <span v-if="p.emoji" aria-hidden="true">{{ p.emoji }}</span>
              <span
                v-if="p.color"
                class="size-3 rounded-full shrink-0"
                :style="{ backgroundColor: p.color }"
              />
              <span data-testid="project-name">{{ p.name }}</span>
            </h2>

            <div class="flex items-center gap-1.5">
              <span
                v-if="p.is_owner === false || (p.shares && p.shares.length > 0)"
                class="badge badge-xs badge-info/20 text-info gap-1 text-[11px] py-2 px-2"
                :title="
                  p.is_owner === false && p.owner
                    ? t('projects.sharedBy', { name: p.owner.name || p.owner.email })
                    : t('projects.shared')
                "
                data-testid="project-shared-badge"
              >
                <Icon icon="lucide:users" class="size-3" />
                <span v-if="p.is_owner === false && p.owner">
                  {{ t('projects.sharedBy', { name: p.owner.name || p.owner.email }) }}
                </span>
                <span v-else> {{ t('projects.shared') }} ({{ p.shares?.length ?? 0 }}) </span>
              </span>
              <span
                v-if="p.is_sensitive"
                class="badge badge-xs badge-neutral gap-1 text-[11px] py-2 px-2"
                :title="t('projects.sensitive')"
              >
                <Icon icon="lucide:lock" class="size-3" />
                <span>{{ t('projects.sensitive') }}</span>
              </span>
              <span
                class="badge badge-xs text-[11px] py-2 px-2 font-medium"
                :class="{
                  'badge-primary': p.status === 'active',
                  'badge-success': p.status === 'completed',
                  'badge-ghost': p.status === 'archived',
                }"
              >
                {{ t(`projects.${p.status}`) }}
              </span>
            </div>
          </div>

          <!-- Description if any -->
          <p
            v-if="p.description"
            class="text-sm text-base-content/70 line-clamp-2"
            data-testid="project-description"
          >
            {{ p.description }}
          </p>

          <!-- Badges (Category & Target Date) -->
          <div class="flex flex-wrap items-center gap-2 text-xs text-base-content/70 pt-1">
            <span
              v-if="p.category_id && categoriesById.has(p.category_id)"
              class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-base-200 font-medium"
              :style="
                categoriesById.get(p.category_id)!.color
                  ? { borderLeft: `3px solid ${categoriesById.get(p.category_id)!.color}` }
                  : {}
              "
              data-testid="project-category"
            >
              <Icon icon="lucide:tags" class="size-3" />
              <span>{{ categoriesById.get(p.category_id)!.name }}</span>
            </span>

            <span
              v-if="p.target_date"
              class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-base-200 font-medium"
              data-testid="project-target-date"
            >
              <Icon icon="lucide:calendar" class="size-3" />
              <span>{{ formatCustomDate(p.target_date) }}</span>
            </span>
          </div>

          <!-- Progress -->
          <div class="space-y-1.5 pt-2">
            <div class="flex items-center justify-between text-xs text-base-content/70 font-medium">
              <span>{{ t('projects.progress') }}</span>
              <span data-testid="project-progress">
                {{ getProjectStats(p.id).completed }}/{{ getProjectStats(p.id).total }} ({{
                  getProjectStats(p.id).percent
                }}%)
              </span>
            </div>
            <progress
              class="progress progress-primary w-full h-1.5"
              :value="getProjectStats(p.id).completed"
              :max="getProjectStats(p.id).total || 1"
            />
          </div>
        </div>

        <!-- Card Footer Quick Actions -->
        <div
          class="bg-base-200/40 px-5 py-2.5 border-t border-base-200 flex items-center justify-end gap-2 text-xs"
          @click.stop
        >
          <!-- Active project actions -->
          <template v-if="p.status === 'active'">
            <button
              type="button"
              class="btn btn-xs btn-ghost gap-1 hover:text-success"
              data-testid="quick-action-complete"
              @click.stop="updateProjectStatus(p, 'completed')"
            >
              <Icon icon="lucide:check" class="size-3.5" />
              <span>{{ t('projects.complete') }}</span>
            </button>
            <button
              type="button"
              class="btn btn-xs btn-ghost gap-1 hover:text-warning"
              data-testid="quick-action-archive"
              @click.stop="updateProjectStatus(p, 'archived')"
            >
              <Icon icon="lucide:archive" class="size-3.5" />
              <span>{{ t('projects.archive') }}</span>
            </button>
          </template>

          <!-- Completed project actions -->
          <template v-else-if="p.status === 'completed'">
            <button
              type="button"
              class="btn btn-xs btn-ghost gap-1 hover:text-primary"
              data-testid="quick-action-reopen"
              @click.stop="updateProjectStatus(p, 'active')"
            >
              <Icon icon="lucide:undo-2" class="size-3.5" />
              <span>{{ t('projects.reopen') }}</span>
            </button>
            <button
              type="button"
              class="btn btn-xs btn-ghost gap-1 hover:text-warning"
              data-testid="quick-action-archive"
              @click.stop="updateProjectStatus(p, 'archived')"
            >
              <Icon icon="lucide:archive" class="size-3.5" />
              <span>{{ t('projects.archive') }}</span>
            </button>
          </template>

          <!-- Archived project actions -->
          <template v-else-if="p.status === 'archived'">
            <button
              type="button"
              class="btn btn-xs btn-ghost gap-1 hover:text-primary"
              data-testid="quick-action-reopen"
              @click.stop="updateProjectStatus(p, 'active')"
            >
              <Icon icon="lucide:undo-2" class="size-3.5" />
              <span>{{ t('projects.reopen') }}</span>
            </button>
          </template>
        </div>
      </div>
    </div>

    <!-- New Project Modal -->
    <div
      v-if="showNewModal"
      class="modal modal-open"
      role="dialog"
      aria-modal="true"
      data-testid="modal-new-project"
      @keydown.esc.prevent="closeNewModal"
    >
      <div class="modal-box max-w-lg p-6 space-y-4">
        <div class="flex items-center justify-between pb-2 border-b border-base-200">
          <h3 class="text-lg font-bold flex items-center gap-2">
            <Icon icon="lucide:folder-plus" class="size-5 text-primary" />
            <span>{{ t('projects.new') }}</span>
          </h3>
          <button type="button" class="btn btn-sm btn-ghost btn-circle" @click="closeNewModal">
            <Icon icon="lucide:x" class="size-4" />
          </button>
        </div>

        <div v-if="modalError" class="alert alert-error text-xs shadow-xs py-2">
          <span>{{ modalError }}</span>
        </div>

        <form class="space-y-4" @submit.prevent="handleCreateProject">
          <!-- Name (required) -->
          <div class="form-control">
            <label class="label py-1">
              <span class="label-text font-medium">{{ t('projects.name') }} *</span>
            </label>
            <input
              v-model="form.name"
              type="text"
              class="input input-bordered w-full"
              :placeholder="t('projects.name')"
              required
              data-testid="input-project-name"
            />
          </div>

          <!-- Description -->
          <div class="form-control">
            <label class="label py-1">
              <span class="label-text font-medium">{{ t('projects.description') }}</span>
            </label>
            <textarea
              v-model="form.description"
              class="textarea textarea-bordered w-full"
              :placeholder="t('projects.description')"
              rows="3"
              data-testid="input-project-description"
            />
          </div>

          <!-- Color presets -->
          <div class="form-control flex flex-col gap-1.5">
            <span class="label-text font-medium">{{ t('emoji.label') }}</span>
            <EmojiPicker v-model="form.emoji" testid="project-emoji" />
          </div>

          <div class="form-control space-y-1">
            <label class="label py-1">
              <span class="label-text font-medium">{{ t('projects.color') }}</span>
            </label>
            <div class="flex flex-wrap items-center gap-2">
              <button
                v-for="preset in COLOR_PRESETS"
                :key="preset.label"
                type="button"
                class="size-6 rounded-full border-2 transition-transform hover:scale-110 flex items-center justify-center cursor-pointer"
                :class="
                  form.color === preset.value
                    ? 'border-primary ring-2 ring-primary/40'
                    : 'border-base-300'
                "
                :style="
                  preset.value
                    ? { backgroundColor: preset.value }
                    : { backgroundColor: 'transparent' }
                "
                :title="preset.label"
                data-testid="project-color-preset"
                @click="form.color = preset.value"
              >
                <Icon
                  v-if="form.color === preset.value"
                  icon="lucide:check"
                  class="size-3.5"
                  :class="preset.value ? 'text-white' : 'text-base-content'"
                />
              </button>
            </div>
          </div>

          <!-- Category Selector -->
          <div class="form-control">
            <label class="label py-1">
              <span class="label-text font-medium">{{ t('projects.category') }}</span>
            </label>
            <select
              v-model="form.category_id"
              class="select select-bordered w-full"
              data-testid="select-project-category"
            >
              <option :value="null">{{ t('projects.noCategory') }}</option>
              <option v-for="c in categories" :key="c.id" :value="c.id">
                {{ c.name }}
              </option>
            </select>
          </div>

          <!-- Target Date -->
          <div class="form-control">
            <label class="label py-1">
              <span class="label-text font-medium">{{ t('projects.targetDate') }}</span>
            </label>
            <DateInput
              v-model="form.target_date"
              clearable
              data-testid="input-project-target-date"
            />
          </div>

          <!-- Sensitive Toggle -->
          <div class="form-control pt-1">
            <label class="label cursor-pointer justify-start gap-3 py-1">
              <input
                v-model="form.is_sensitive"
                type="checkbox"
                class="checkbox checkbox-primary checkbox-sm"
                data-testid="input-project-sensitive"
              />
              <span class="label-text font-medium flex items-center gap-1.5">
                <Icon icon="lucide:lock" class="size-4 text-warning" />
                {{ t('projects.sensitive') }}
              </span>
            </label>
          </div>

          <!-- Dialog Actions -->
          <div class="modal-action pt-4 border-t border-base-200">
            <button
              type="button"
              class="btn btn-ghost"
              data-testid="btn-cancel-project"
              @click="closeNewModal"
            >
              {{ t('common.cancel') }}
            </button>
            <button
              type="submit"
              class="btn btn-primary gap-2"
              :disabled="saving || !form.name.trim()"
              data-testid="btn-save-project"
            >
              <span v-if="saving" class="loading loading-spinner loading-xs" />
              <span>{{ t('common.save') }}</span>
            </button>
          </div>
        </form>
      </div>
      <div class="modal-backdrop bg-black/40" @click="closeNewModal" />
    </div>
  </div>
</template>
