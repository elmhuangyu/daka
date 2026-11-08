<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Icon } from '@iconify/vue'
import {
  createCategory, describeError, listCategories, removeCategory, updateCategory, type CategoryDto,
} from '../../api'
import { useSettingsStore } from '../../stores/settings'

const store = useSettingsStore()
const cats = ref<CategoryDto[]>([])
const error = ref('')
const notice = ref('')
const newName = ref('')
const newSensitive = ref(false)

const editingId = ref<number | null>(null)
const editName = ref('')

// Pending confirmations. `expose` is the freeze/exposure choice for a category
// that stops being sensitive (freeze: its items become individually sensitive).
const unmarking = ref<CategoryDto | null>(null)
const deleting = ref<CategoryDto | null>(null)
const expose = ref(false)
const target = ref<'uncategorize' | number>('uncategorize')

async function load() {
  try {
    cats.value = (await listCategories()) ?? []
  } catch (e) {
    error.value = describeError(e)
  }
}
onMounted(load)

async function run(fn: () => Promise<void>) {
  error.value = ''
  notice.value = ''
  try {
    await fn()
    await load()
  } catch (e) {
    error.value = describeError(e)
  }
}

const create = () =>
  run(async () => {
    if (!newName.value.trim()) return
    await createCategory(newName.value.trim(), newSensitive.value)
    newName.value = ''
    newSensitive.value = false
  })

function startEdit(c: CategoryDto) {
  editingId.value = c.id
  editName.value = c.name
}

const rename = (c: CategoryDto) =>
  run(async () => {
    await updateCategory(c.id, { name: editName.value.trim() })
    editingId.value = null
  })

function toggleSensitive(c: CategoryDto) {
  if (c.is_sensitive) {
    expose.value = false
    unmarking.value = c
  } else {
    void run(async () => void (await updateCategory(c.id, { is_sensitive: true })))
  }
}

const confirmUnmark = () => {
  const c = unmarking.value!
  unmarking.value = null
  return run(async () => {
    const r = await updateCategory(c.id, { is_sensitive: false }, expose.value)
    const n = r.affected_items ?? 0
    if (n > 0) notice.value = expose.value ? `${n} item(s) are now exposed.` : `${n} item(s) were marked sensitive to keep them protected.`
  })
}

function startDelete(c: CategoryDto) {
  expose.value = false
  target.value = 'uncategorize'
  deleting.value = c
}

const confirmDelete = () => {
  const c = deleting.value!
  const t = target.value
  deleting.value = null
  return run(async () => {
    const dest = typeof t === 'number' ? cats.value.find((x) => x.id === t) : undefined
    const leaves = c.is_sensitive && !dest?.is_sensitive
    await removeCategory(c.id, typeof t === 'number' ? { moveTo: t } : { uncategorize: true }, leaves && expose.value)
  })
}
</script>

<template>
  <section class="card bg-base-200" data-testid="section-categories">
    <div class="card-body gap-3">
      <h2 class="card-title">Categories</h2>
      <p v-if="error" class="text-error" role="alert" data-testid="cat-error">{{ error }}</p>
      <p v-if="notice" class="text-info" data-testid="cat-notice">{{ notice }}</p>
      <ul class="flex flex-col gap-2">
        <li v-for="c in cats" :key="c.id" class="flex flex-wrap items-center gap-2" data-testid="cat-row">
          <template v-if="editingId === c.id">
            <input v-model="editName" class="input input-bordered input-sm" maxlength="64" data-testid="cat-edit-name" />
            <button class="btn btn-sm btn-primary" data-testid="cat-edit-save" @click="rename(c)">Save</button>
            <button class="btn btn-sm btn-ghost" @click="editingId = null">Cancel</button>
          </template>
          <template v-else>
            <span class="font-medium" data-testid="cat-name">{{ store.categoryLabel(c) }}</span>
            <span v-if="c.is_sensitive" class="badge badge-warning gap-1"><Icon icon="lucide:lock" aria-hidden="true" />Sensitive</span>
            <span class="flex-1" />
            <button
              class="btn btn-sm btn-ghost" :disabled="store.masking && c.is_sensitive"
              :aria-label="`Rename ${store.categoryLabel(c)}`" @click="startEdit(c)"
            >Rename</button>
            <button class="btn btn-sm btn-ghost" data-testid="cat-toggle" @click="toggleSensitive(c)">
              {{ c.is_sensitive ? 'Make not sensitive' : 'Make sensitive' }}
            </button>
            <button
              class="btn btn-sm btn-ghost text-error" data-testid="cat-delete"
              :aria-label="`Delete ${store.categoryLabel(c)}`" @click="startDelete(c)"
            ><Icon icon="lucide:trash-2" aria-hidden="true" /></button>
          </template>
        </li>
        <li v-if="!cats.length" class="opacity-70">No categories yet.</li>
      </ul>
      <form class="flex flex-wrap items-center gap-2" @submit.prevent="create">
        <input v-model="newName" class="input input-bordered input-sm" placeholder="New category" maxlength="64" data-testid="cat-new-name" />
        <label class="label cursor-pointer gap-2">
          <input v-model="newSensitive" type="checkbox" class="checkbox checkbox-sm" data-testid="cat-new-sensitive" />
          <span class="label-text">Sensitive</span>
        </label>
        <button class="btn btn-sm btn-primary" type="submit" data-testid="cat-create">Add</button>
      </form>
    </div>

    <div v-if="unmarking" class="modal modal-open" role="dialog" aria-modal="true" data-testid="cat-unmark-dialog">
      <div class="modal-box">
        <h3 class="text-lg font-bold">Make this category not sensitive?</h3>
        <p class="mt-2">Items in it are no longer protected by the category. Choose what happens to them:</p>
        <label class="label cursor-pointer justify-start gap-2">
          <input v-model="expose" type="radio" class="radio" :value="false" data-testid="cat-freeze" />
          <span>Keep them protected (each item is marked sensitive)</span>
        </label>
        <label class="label cursor-pointer justify-start gap-2">
          <input v-model="expose" type="radio" class="radio" :value="true" data-testid="cat-expose" />
          <span>Expose them (their names become visible to feeds and notifications)</span>
        </label>
        <div class="modal-action">
          <button class="btn" @click="unmarking = null">Cancel</button>
          <button class="btn btn-primary" data-testid="cat-unmark-confirm" @click="confirmUnmark">Confirm</button>
        </div>
      </div>
    </div>

    <div v-if="deleting" class="modal modal-open" role="dialog" aria-modal="true" data-testid="cat-delete-dialog">
      <div class="modal-box">
        <h3 class="text-lg font-bold">Delete {{ store.categoryLabel(deleting) }}?</h3>
        <p class="mt-2">What should happen to the items in this category?</p>
        <select v-model="target" class="select select-bordered mt-2 w-full" data-testid="cat-delete-target">
          <option value="uncategorize">Leave them uncategorized</option>
          <option v-for="c in cats.filter((x) => x.id !== deleting!.id)" :key="c.id" :value="c.id">
            Move to {{ store.categoryLabel(c) }}
          </option>
        </select>
        <div v-if="deleting.is_sensitive" class="mt-3">
          <p>This category is sensitive. Items that do not land in another sensitive category:</p>
          <label class="label cursor-pointer justify-start gap-2">
            <input v-model="expose" type="radio" class="radio" :value="false" />
            <span>Stay protected (marked sensitive)</span>
          </label>
          <label class="label cursor-pointer justify-start gap-2">
            <input v-model="expose" type="radio" class="radio" :value="true" data-testid="cat-delete-expose" />
            <span>Become exposed</span>
          </label>
        </div>
        <div class="modal-action">
          <button class="btn" @click="deleting = null">Cancel</button>
          <button class="btn btn-error" data-testid="cat-delete-confirm" @click="confirmDelete">Delete</button>
        </div>
      </div>
    </div>
  </section>
</template>
