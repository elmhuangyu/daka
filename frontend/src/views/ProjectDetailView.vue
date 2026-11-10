<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Icon } from '@iconify/vue'
import * as api from '../api'
import type { Category, ItemDetail, Project, ProjectStatus, UpdateProjectRequest } from '../api'
import DateInput from '../components/DateInput.vue'
import { useI18n } from '../i18n'
import { formatCustomDate } from '../i18n/format'
import { usePrivacyStore } from '../stores/privacy'
import { useProjectsStore } from '../stores/projects'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const privacy = usePrivacyStore()
const projectsStore = useProjectsStore()

const projectId = computed(() => Number(route.params.id))
const project = ref<Project | null>(null)
const categories = ref<Category[]>([])
const tasks = ref<ItemDetail[]>([])

const loading = ref(true)
const error = ref('')

// Edit Project Modal State
const showEditModal = ref(false)
const editSaving = ref(false)
const editError = ref('')
const editForm = reactive({
  name: '',
  description: '',
  color: '',
  category_id: null as number | null,
  target_date: '',
  is_sensitive: false,
})

// Delete Project Modal State
const showDeleteModal = ref(false)
const deleteMode = ref<'unlink' | 'cascade'>('unlink')
const deleting = ref(false)
const deleteError = ref('')

// Quick Add Task State
const newTaskName = ref('')
const newTaskDate = ref('')
const addingTask = ref(false)
const quickAddError = ref('')

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

function getTodayIso(): string {
  const now = new Date()
  const y = now.getFullYear()
  const m = String(now.getMonth() + 1).padStart(2, '0')
  const d = String(now.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

function offsetIso(dateStr: string, days: number): string {
  const d = new Date(dateStr + 'T00:00:00')
  d.setDate(d.getDate() + days)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

const todayIso = computed(() => getTodayIso())
const tomorrowIso = computed(() => offsetIso(todayIso.value, 1))

const categoriesById = computed(() => {
  const map = new Map<number, Category>()
  for (const c of categories.value) {
    map.set(c.id, c)
  }
  return map
})

const rootTasks = computed(() => {
  const ids = new Set(tasks.value.map((t) => t.id))
  return tasks.value.filter((t) => !t.parent_id || !ids.has(t.parent_id))
})
const childrenByParent = computed(() => {
  const map = new Map<number, ItemDetail[]>()
  for (const t of tasks.value) {
    if (!t.parent_id) continue
    const list = map.get(t.parent_id) ?? []
    list.push(t)
    map.set(t.parent_id, list)
  }
  return map
})

const totalTasks = computed(() => tasks.value.length)
const completedTasks = computed(
  () => tasks.value.filter((task) => (task.stats?.counts?.total ?? 0) > 0).length,
)
const progressPercent = computed(() =>
  totalTasks.value > 0 ? Math.round((completedTasks.value / totalTasks.value) * 100) : 0,
)

let seq = 0

async function loadProject() {
  if (!projectId.value || isNaN(projectId.value)) {
    error.value = t('projects.invalidId')
    loading.value = false
    return
  }
  const my = ++seq
  loading.value = true
  error.value = ''
  try {
    const [proj, cats, itemsRes] = await Promise.all([
      api.getProject(projectId.value, privacy.redact),
      api.getCategories(),
      api.listProjectItems(projectId.value, { redact: privacy.redact }),
    ])
    if (my !== seq) return
    project.value = proj
    categories.value = (cats.categories ?? []).map((c) =>
      privacy.redact && c.is_sensitive ? { ...c, name: t('common.privateCategory'), icon: '' } : c,
    )
    tasks.value = itemsRes.items
  } catch (e) {
    if (my !== seq) return
    error.value = api.describeError(e)
  } finally {
    if (my === seq) {
      loading.value = false
    }
  }
}

onMounted(() => {
  loadProject()
})

watch(
  () => privacy.redact,
  (on) => {
    if (on) {
      seq++
      project.value = null
      tasks.value = []
      categories.value = []
    }
    void loadProject()
  },
  { flush: 'sync' },
)

function goBack() {
  router.push('/projects')
}

// Status Updates
async function updateStatus(newStatus: ProjectStatus) {
  if (!project.value) return
  try {
    const updated = await projectsStore.updateProject(project.value.id, { status: newStatus })
    if (privacy.redact) {
      await loadProject()
    } else {
      project.value = updated
    }
  } catch (e) {
    error.value = api.describeError(e)
  }
}

// Edit Modal
function openEditModal() {
  if (!project.value) return
  editForm.name = project.value.name
  editForm.description = project.value.description
  editForm.color = project.value.color
  editForm.category_id = project.value.category_id
  editForm.target_date = project.value.target_date || ''
  editForm.is_sensitive = project.value.is_sensitive
  editError.value = ''
  showEditModal.value = true
}

function closeEditModal() {
  showEditModal.value = false
  editError.value = ''
}

async function handleSaveEdit() {
  if (!project.value) return
  const name = editForm.name.trim()
  if (!name) {
    editError.value = t('projects.nameRequired')
    return
  }
  editSaving.value = true
  editError.value = ''
  try {
    const req: UpdateProjectRequest = {
      name,
      description: editForm.description.trim() || undefined,
      color: editForm.color,
      category_id: editForm.category_id,
      target_date: editForm.target_date || null,
      is_sensitive: editForm.is_sensitive,
    }
    const updated = await projectsStore.updateProject(project.value.id, req)
    if (privacy.redact) {
      await loadProject()
    } else {
      project.value = updated
    }
    closeEditModal()
  } catch (e) {
    editError.value = api.describeError(e)
  } finally {
    editSaving.value = false
  }
}

// Delete Modal
function openDeleteModal() {
  deleteMode.value = 'unlink'
  deleteError.value = ''
  showDeleteModal.value = true
}

function closeDeleteModal() {
  showDeleteModal.value = false
  deleteError.value = ''
}

async function handleConfirmDelete() {
  if (!project.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    if (tasks.value.length > 0) {
      await projectsStore.deleteProject(project.value.id, {
        unlink: deleteMode.value === 'unlink',
        cascade: deleteMode.value === 'cascade',
      })
    } else {
      await projectsStore.deleteProject(project.value.id)
    }
    closeDeleteModal()
    router.push('/projects')
  } catch (e) {
    deleteError.value = api.describeError(e)
  } finally {
    deleting.value = false
  }
}

// Task Management: Add Task
async function handleQuickAddTask() {
  const name = newTaskName.value.trim()
  if (!name || !project.value) return
  addingTask.value = true
  quickAddError.value = ''
  try {
    const dueDate = newTaskDate.value ? newTaskDate.value.trim() : null
    await api.createTodoItem(name, dueDate, project.value.id)
    newTaskName.value = ''
    newTaskDate.value = ''
    // Refresh project and task list
    await loadProject()
  } catch (e) {
    quickAddError.value = api.describeError(e)
  } finally {
    addingTask.value = false
  }
}

// Task Management: Sub-tasks
const subtaskParentId = ref<number | null>(null)
const subtaskName = ref('')
const addingSubtask = ref(false)

function openSubtaskForm(task: ItemDetail) {
  subtaskParentId.value = task.id
  subtaskName.value = ''
}

function closeSubtaskForm() {
  subtaskParentId.value = null
  subtaskName.value = ''
}

async function handleAddSubtask(parent: ItemDetail) {
  const name = subtaskName.value.trim()
  if (!name || !project.value) return
  addingSubtask.value = true
  quickAddError.value = ''
  try {
    await api.createTodoItem(name, null, project.value.id, parent.id)
    subtaskName.value = ''
    await refreshTasks()
  } catch (e) {
    quickAddError.value = api.describeError(e)
  } finally {
    addingSubtask.value = false
  }
}

async function refreshTasks() {
  if (!projectId.value) return
  const res = await api.listProjectItems(projectId.value, { redact: privacy.redact })
  tasks.value = res.items
}

// Task Management: Assign / change a due date
async function changeTaskDate(task: ItemDetail, value: string) {
  if (taskDueDate(task) === (value || null)) return
  try {
    await api.setTodoDueDate(task.id, value || null)
    await refreshTasks()
  } catch (e) {
    error.value = api.describeError(e)
  }
}

// Task Management: Toggle Task
async function toggleTask(task: ItemDetail) {
  const isDone = (task.stats?.counts?.total ?? 0) > 0
  const nextDone = !isDone
  // Optimistic update
  const originalStats = task.stats
  task.stats = {
    ...task.stats,
    counts: {
      last_30_days: nextDone ? 1 : 0,
      this_year: nextDone ? 1 : 0,
      total: nextDone ? 1 : 0,
    },
  }
  try {
    await api.setCheckin(task.id, nextDone)
  } catch (e) {
    task.stats = originalStats
    error.value = api.describeError(e)
  }
}

function removeTaskLocally(task: ItemDetail) {
  tasks.value = tasks.value.filter((t) => t.id !== task.id && t.parent_id !== task.id)
}

// Task Management: Unlink Task (sub-tasks move with their parent)
async function unlinkTask(task: ItemDetail) {
  try {
    await api.updateItem(task.id, { project_id: null })
    removeTaskLocally(task)
  } catch (e) {
    error.value = api.describeError(e)
  }
}

// Task Management: Delete Task (the backend also deletes its sub-tasks)
async function deleteTask(task: ItemDetail) {
  try {
    await api.deleteItem(task.id)
    removeTaskLocally(task)
  } catch (e) {
    error.value = api.describeError(e)
  }
}

function taskDueDate(task: ItemDetail): string | null {
  if (task.current_rule.schedule_type === 'fixed_rrule' && task.current_rule.dtstart) {
    return task.current_rule.dtstart
  }
  return null
}
</script>

<template>
  <div class="space-y-6 max-w-5xl mx-auto p-4 sm:p-6" data-testid="project-detail-view">
    <!-- Back Button -->
    <div>
      <button
        type="button"
        class="btn btn-sm btn-ghost gap-1.5 -ml-2 text-base-content/70 hover:text-base-content"
        data-testid="btn-back-projects"
        @click="goBack"
      >
        <Icon icon="lucide:chevron-left" class="size-4" />
        <span>{{ t('projects.back') }}</span>
      </button>
    </div>

    <!-- Error Alert -->
    <div v-if="error" class="alert alert-error text-sm shadow-sm" data-testid="project-error">
      <Icon icon="lucide:triangle-alert" class="size-5 shrink-0" />
      <span>{{ error }}</span>
    </div>

    <!-- Loading State -->
    <div v-if="loading" class="flex justify-center py-16">
      <span class="loading loading-spinner loading-md text-primary" />
    </div>

    <!-- Project Content -->
    <template v-else-if="project">
      <!-- Project Header Card -->
      <div
        class="card bg-base-100 border border-base-300 shadow-xs overflow-hidden"
        :style="project.color ? { borderLeftColor: project.color, borderLeftWidth: '4px' } : {}"
      >
        <div class="card-body p-5 sm:p-6 space-y-4">
          <!-- Top Row: Title, Badges & Top Actions -->
          <div class="flex flex-col sm:flex-row sm:items-start justify-between gap-4">
            <div class="space-y-1.5">
              <div class="flex items-center gap-2.5 flex-wrap">
                <span
                  v-if="project.color"
                  class="size-3.5 rounded-full shrink-0"
                  :style="{ backgroundColor: project.color }"
                />
                <h1
                  class="text-2xl font-bold tracking-tight text-base-content"
                  data-testid="project-name"
                >
                  {{ project.name }}
                </h1>

                <!-- Status Badge -->
                <span
                  class="badge text-xs font-semibold py-2.5 px-2.5"
                  :class="{
                    'badge-primary': project.status === 'active',
                    'badge-success': project.status === 'completed',
                    'badge-ghost': project.status === 'archived',
                  }"
                  data-testid="project-status"
                >
                  {{ t(`projects.${project.status}`) }}
                </span>

                <!-- Sensitive Badge -->
                <span
                  v-if="project.is_sensitive"
                  class="badge badge-neutral gap-1 text-xs py-2.5 px-2.5"
                  :title="t('projects.sensitive')"
                  data-testid="project-sensitive-badge"
                >
                  <Icon icon="lucide:lock" class="size-3 text-warning" />
                  <span>{{ t('projects.sensitive') }}</span>
                </span>
              </div>

              <!-- Description -->
              <p
                v-if="project.description"
                class="text-sm text-base-content/80 whitespace-pre-line pt-1"
                data-testid="project-description"
              >
                {{ project.description }}
              </p>
            </div>

            <!-- Action Buttons -->
            <div class="flex flex-wrap items-center gap-1.5 shrink-0">
              <!-- Complete / Reopen -->
              <button
                v-if="project.status === 'active'"
                type="button"
                class="btn btn-sm btn-outline btn-success gap-1.5"
                data-testid="btn-complete-project"
                @click="updateStatus('completed')"
              >
                <Icon icon="lucide:check" class="size-4" />
                <span>{{ t('projects.complete') }}</span>
              </button>
              <button
                v-else-if="project.status === 'completed'"
                type="button"
                class="btn btn-sm btn-outline btn-primary gap-1.5"
                data-testid="btn-reopen-project"
                @click="updateStatus('active')"
              >
                <Icon icon="lucide:undo-2" class="size-4" />
                <span>{{ t('projects.reopen') }}</span>
              </button>
              <button
                v-else-if="project.status === 'archived'"
                type="button"
                class="btn btn-sm btn-outline btn-primary gap-1.5"
                data-testid="btn-reopen-project"
                @click="updateStatus('active')"
              >
                <Icon icon="lucide:undo-2" class="size-4" />
                <span>{{ t('projects.reopen') }}</span>
              </button>

              <!-- Archive / Unarchive -->
              <button
                v-if="project.status !== 'archived'"
                type="button"
                class="btn btn-sm btn-ghost gap-1.5 text-base-content/70 hover:text-warning"
                data-testid="btn-archive-project"
                @click="updateStatus('archived')"
              >
                <Icon icon="lucide:archive" class="size-4" />
                <span>{{ t('projects.archive') }}</span>
              </button>

              <!-- Edit -->
              <button
                type="button"
                class="btn btn-sm btn-ghost gap-1.5 text-base-content/70 hover:text-primary"
                data-testid="btn-edit-project"
                @click="openEditModal"
              >
                <Icon icon="lucide:pencil" class="size-4" />
                <span>{{ t('projects.edit') }}</span>
              </button>

              <!-- Delete -->
              <button
                type="button"
                class="btn btn-sm btn-ghost gap-1.5 text-error hover:bg-error/10"
                data-testid="btn-delete-project"
                @click="openDeleteModal"
              >
                <Icon icon="lucide:trash-2" class="size-4" />
                <span>{{ t('projects.delete') }}</span>
              </button>
            </div>
          </div>

          <!-- Metadata Badges (Category & Target Date) -->
          <div class="flex flex-wrap items-center gap-3 text-xs text-base-content/70 pt-1">
            <span
              v-if="project.category_id && categoriesById.has(project.category_id)"
              class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-base-200 font-medium"
              :style="
                categoriesById.get(project.category_id)!.color
                  ? { borderLeft: `3px solid ${categoriesById.get(project.category_id)!.color}` }
                  : {}
              "
              data-testid="project-category-badge"
            >
              <Icon icon="lucide:tags" class="size-3.5" />
              <span>{{ categoriesById.get(project.category_id)!.name }}</span>
            </span>

            <span
              v-if="project.target_date"
              class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-base-200 font-medium"
              data-testid="project-target-date-badge"
            >
              <Icon icon="lucide:calendar" class="size-3.5" />
              <span
                >{{ t('projects.targetDate') }}: {{ formatCustomDate(project.target_date) }}</span
              >
            </span>
          </div>

          <!-- Progress Bar & Stats -->
          <div class="space-y-1.5 pt-2 border-t border-base-200">
            <div
              class="flex items-center justify-between text-xs font-semibold text-base-content/80"
            >
              <span>{{ t('projects.progress') }}</span>
              <span data-testid="project-progress-stats">
                {{
                  t('projects.progressStats', {
                    completed: completedTasks,
                    total: totalTasks,
                    percent: progressPercent,
                  })
                }}
              </span>
            </div>
            <progress
              class="progress progress-primary w-full h-2"
              :value="completedTasks"
              :max="totalTasks || 1"
            />
          </div>
        </div>
      </div>

      <!-- Task Management Section -->
      <div class="space-y-4">
        <h2 class="text-lg font-bold text-base-content flex items-center gap-2">
          <Icon icon="lucide:list-checks" class="size-5 text-primary" />
          <span>{{ t('projects.tasks') }}</span>
          <span class="badge badge-sm font-semibold">{{ totalTasks }}</span>
        </h2>

        <!-- Quick Add Task Bar -->
        <div class="card bg-base-100 border border-base-300 p-3 shadow-xs">
          <form class="space-y-2" @submit.prevent="handleQuickAddTask">
            <div class="flex items-center gap-2">
              <input
                v-model="newTaskName"
                type="text"
                class="input input-sm input-bordered min-w-0 flex-1"
                :placeholder="t('projects.addTaskPlaceholder')"
                autocomplete="off"
                data-testid="input-quick-task-name"
              />
              <button
                type="submit"
                class="btn btn-sm btn-primary gap-1 shadow-xs shrink-0"
                :disabled="!newTaskName.trim() || addingTask"
                data-testid="btn-quick-add-task"
              >
                <span v-if="addingTask" class="loading loading-spinner loading-xs" />
                <Icon v-else icon="lucide:plus" class="size-4" />
                <span class="hidden sm:inline">{{ t('projects.addTask') }}</span>
              </button>
            </div>

            <div class="flex flex-wrap items-center gap-1.5">
              <button
                type="button"
                class="btn btn-xs rounded-md"
                :class="!newTaskDate ? 'btn-primary' : 'btn-ghost text-base-content/70'"
                @click="newTaskDate = ''"
              >
                {{ t('projects.noDueDate') }}
              </button>
              <button
                type="button"
                class="btn btn-xs rounded-md"
                :class="newTaskDate === todayIso ? 'btn-primary' : 'btn-ghost text-base-content/70'"
                @click="newTaskDate = todayIso"
              >
                {{ t('projects.today') }}
              </button>
              <button
                type="button"
                class="btn btn-xs rounded-md"
                :class="
                  newTaskDate === tomorrowIso ? 'btn-primary' : 'btn-ghost text-base-content/70'
                "
                @click="newTaskDate = tomorrowIso"
              >
                {{ t('projects.tomorrow') }}
              </button>
              <div class="w-40 sm:w-44">
                <DateInput v-model="newTaskDate" clearable data-testid="input-quick-task-date" />
              </div>
            </div>
          </form>

          <div v-if="quickAddError" class="text-xs text-error px-1 pt-2">
            {{ quickAddError }}
          </div>
        </div>

        <!-- Task List -->
        <div
          v-if="tasks.length === 0"
          class="rounded-xl border border-dashed border-base-300 p-8 text-center"
          data-testid="tasks-empty"
        >
          <p class="text-sm text-base-content/60">{{ t('projects.noTasks') }}</p>
        </div>

        <div v-else class="space-y-2" data-testid="project-tasks-list">
          <template v-for="root in rootTasks" :key="root.id">
            <template
              v-for="entry in [
                { task: root, depth: 0 },
                ...(childrenByParent.get(root.id) ?? []).map((c) => ({ task: c, depth: 1 })),
              ]"
              :key="entry.task.id"
            >
              <div
                class="group rounded-xl border border-base-300 bg-base-100 p-3 sm:px-4 flex flex-wrap items-center gap-x-3 gap-y-2 transition-colors hover:bg-base-200/40"
                :class="entry.depth ? 'ml-6 sm:ml-8' : ''"
                :data-testid="`task-item-${entry.task.id}`"
              >
                <div class="flex items-center gap-3 min-w-0 flex-1 basis-48">
                  <input
                    type="checkbox"
                    class="checkbox checkbox-sm checkbox-primary"
                    :checked="(entry.task.stats?.counts?.total ?? 0) > 0"
                    :data-testid="`task-checkbox-${entry.task.id}`"
                    @change="toggleTask(entry.task)"
                  />
                  <span
                    class="text-sm font-medium truncate"
                    :class="{
                      'line-through text-base-content/40':
                        (entry.task.stats?.counts?.total ?? 0) > 0,
                    }"
                    data-testid="task-name"
                  >
                    {{ entry.task.name }}
                  </span>
                </div>

                <div class="flex items-center gap-1.5 shrink-0 ml-auto">
                  <div class="w-40 sm:w-44 shrink-0" :data-testid="`task-date-${entry.task.id}`">
                    <DateInput
                      :model-value="taskDueDate(entry.task) ?? ''"
                      clearable
                      :placeholder="t('projects.setDate')"
                      :aria-label="t('projects.setDate')"
                      @update:model-value="changeTaskDate(entry.task, $event)"
                    />
                  </div>

                  <button
                    v-if="!entry.depth"
                    type="button"
                    class="btn btn-xs btn-ghost btn-circle text-base-content/60 hover:text-primary"
                    :title="t('projects.addSubtask')"
                    :data-testid="`btn-add-subtask-${entry.task.id}`"
                    @click="openSubtaskForm(entry.task)"
                  >
                    <Icon icon="lucide:list-plus" class="size-3.5" />
                  </button>

                  <button
                    v-if="!entry.depth"
                    type="button"
                    class="btn btn-xs btn-ghost btn-circle text-base-content/60 hover:text-warning"
                    :title="t('projects.unlinkTask')"
                    :data-testid="`btn-unlink-task-${entry.task.id}`"
                    @click="unlinkTask(entry.task)"
                  >
                    <Icon icon="lucide:unlink" class="size-3.5" />
                  </button>

                  <button
                    type="button"
                    class="btn btn-xs btn-ghost btn-circle text-base-content/60 hover:text-error"
                    :title="t('projects.deleteTask')"
                    :data-testid="`btn-delete-task-${entry.task.id}`"
                    @click="deleteTask(entry.task)"
                  >
                    <Icon icon="lucide:trash-2" class="size-3.5" />
                  </button>
                </div>
              </div>
            </template>

            <form
              v-if="subtaskParentId === root.id"
              class="ml-6 sm:ml-8 flex items-center gap-2"
              @submit.prevent="handleAddSubtask(root)"
            >
              <input
                v-model="subtaskName"
                type="text"
                class="input input-sm input-bordered min-w-0 flex-1"
                :placeholder="t('projects.subtaskPlaceholder')"
                autocomplete="off"
                autofocus
                @keydown.esc="closeSubtaskForm"
                :data-testid="`input-subtask-name-${root.id}`"
              />
              <button
                type="submit"
                class="btn btn-sm btn-primary"
                :disabled="!subtaskName.trim() || addingSubtask"
                :data-testid="`btn-submit-subtask-${root.id}`"
              >
                {{ t('projects.addTask') }}
              </button>
              <button type="button" class="btn btn-sm btn-ghost" @click="closeSubtaskForm">
                {{ t('common.cancel') }}
              </button>
            </form>
          </template>
        </div>
      </div>

      <!-- Edit Project Modal -->
      <div
        v-if="showEditModal"
        class="modal modal-open"
        role="dialog"
        data-testid="modal-edit-project"
      >
        <div class="modal-box max-w-lg p-6 space-y-4">
          <div class="flex items-center justify-between pb-2 border-b border-base-200">
            <h3 class="text-lg font-bold flex items-center gap-2">
              <Icon icon="lucide:pencil" class="size-5 text-primary" />
              <span>{{ t('projects.edit') }}</span>
            </h3>
            <button type="button" class="btn btn-sm btn-ghost btn-circle" @click="closeEditModal">
              <Icon icon="lucide:x" class="size-4" />
            </button>
          </div>

          <div v-if="editError" class="alert alert-error text-xs shadow-xs py-2">
            <span>{{ editError }}</span>
          </div>

          <form class="space-y-4" @submit.prevent="handleSaveEdit">
            <!-- Name (required) -->
            <div class="form-control">
              <label class="label py-1">
                <span class="label-text font-medium">{{ t('projects.name') }} *</span>
              </label>
              <input
                v-model="editForm.name"
                type="text"
                class="input input-bordered w-full"
                required
                data-testid="input-edit-project-name"
              />
            </div>

            <!-- Description -->
            <div class="form-control">
              <label class="label py-1">
                <span class="label-text font-medium">{{ t('projects.description') }}</span>
              </label>
              <textarea
                v-model="editForm.description"
                class="textarea textarea-bordered w-full"
                rows="3"
                data-testid="input-edit-project-description"
              />
            </div>

            <!-- Color presets -->
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
                    editForm.color === preset.value
                      ? 'border-primary ring-2 ring-primary/40'
                      : 'border-base-300'
                  "
                  :style="
                    preset.value
                      ? { backgroundColor: preset.value }
                      : { backgroundColor: 'transparent' }
                  "
                  :title="preset.label"
                  data-testid="edit-project-color-preset"
                  @click="editForm.color = preset.value"
                >
                  <Icon
                    v-if="editForm.color === preset.value"
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
                v-model="editForm.category_id"
                class="select select-bordered w-full"
                data-testid="select-edit-project-category"
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
                v-model="editForm.target_date"
                clearable
                data-testid="input-edit-project-target-date"
              />
            </div>

            <!-- Sensitive Toggle -->
            <div class="form-control pt-1">
              <label class="label cursor-pointer justify-start gap-3 py-1">
                <input
                  v-model="editForm.is_sensitive"
                  type="checkbox"
                  class="checkbox checkbox-primary checkbox-sm"
                  data-testid="input-edit-project-sensitive"
                />
                <span class="label-text font-medium flex items-center gap-1.5">
                  <Icon icon="lucide:lock" class="size-4 text-warning" />
                  {{ t('projects.sensitive') }}
                </span>
              </label>
            </div>

            <!-- Dialog Actions -->
            <div class="modal-action pt-4 border-t border-base-200">
              <button type="button" class="btn btn-ghost" @click="closeEditModal">
                {{ t('common.cancel') }}
              </button>
              <button
                type="submit"
                class="btn btn-primary gap-2"
                :disabled="editSaving || !editForm.name.trim()"
                data-testid="btn-save-edit-project"
              >
                <span v-if="editSaving" class="loading loading-spinner loading-xs" />
                <span>{{ t('common.save') }}</span>
              </button>
            </div>
          </form>
        </div>
        <div class="modal-backdrop bg-black/40" @click="closeEditModal" />
      </div>

      <!-- Delete Project Modal -->
      <div
        v-if="showDeleteModal"
        class="modal modal-open"
        role="dialog"
        data-testid="modal-delete-project"
      >
        <div class="modal-box max-w-md p-6 space-y-4">
          <div class="flex items-center justify-between pb-2 border-b border-base-200">
            <h3 class="text-lg font-bold flex items-center gap-2 text-error">
              <Icon icon="lucide:trash-2" class="size-5" />
              <span>{{ t('projects.delete') }}</span>
            </h3>
            <button type="button" class="btn btn-sm btn-ghost btn-circle" @click="closeDeleteModal">
              <Icon icon="lucide:x" class="size-4" />
            </button>
          </div>

          <div v-if="deleteError" class="alert alert-error text-xs shadow-xs py-2">
            <span>{{ deleteError }}</span>
          </div>

          <!-- If project has items, show unlink vs cascade choices -->
          <div v-if="tasks.length > 0" class="space-y-3">
            <p class="text-sm text-base-content/80">
              {{ t('projects.deleteWithItemsPrompt', { count: tasks.length }) }}
            </p>
            <div class="space-y-2">
              <label
                class="flex items-center gap-3 p-3 rounded-xl border border-base-300 cursor-pointer hover:bg-base-200/50"
              >
                <input
                  v-model="deleteMode"
                  type="radio"
                  name="delete-mode"
                  value="unlink"
                  class="radio radio-primary radio-sm"
                  data-testid="radio-delete-unlink"
                />
                <span class="text-sm font-medium">{{ t('projects.unlinkTasks') }}</span>
              </label>
              <label
                class="flex items-center gap-3 p-3 rounded-xl border border-base-300 cursor-pointer hover:bg-base-200/50"
              >
                <input
                  v-model="deleteMode"
                  type="radio"
                  name="delete-mode"
                  value="cascade"
                  class="radio radio-error radio-sm"
                  data-testid="radio-delete-cascade"
                />
                <span class="text-sm font-medium text-error">{{
                  t('projects.deleteCascade')
                }}</span>
              </label>
            </div>
          </div>
          <!-- If project has no items -->
          <div v-else>
            <p class="text-sm text-base-content/80">
              {{ t('projects.deleteConfirmPrompt') }}
            </p>
          </div>

          <!-- Dialog Actions -->
          <div class="modal-action pt-4 border-t border-base-200">
            <button type="button" class="btn btn-ghost" @click="closeDeleteModal">
              {{ t('common.cancel') }}
            </button>
            <button
              type="button"
              class="btn btn-error gap-2"
              :disabled="deleting"
              data-testid="btn-confirm-delete-project"
              @click="handleConfirmDelete"
            >
              <span v-if="deleting" class="loading loading-spinner loading-xs" />
              <span>{{ t('common.delete') }}</span>
            </button>
          </div>
        </div>
        <div class="modal-backdrop bg-black/40" @click="closeDeleteModal" />
      </div>
    </template>
  </div>
</template>
