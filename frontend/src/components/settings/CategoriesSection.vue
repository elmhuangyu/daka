<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Icon } from '@iconify/vue'
import {
  createCategory, describeError, listCategories, removeCategory, updateCategory, type CategoryDto,
} from '../../api'
import { useSettingsStore } from '../../stores/settings'
import { useI18n } from '../../i18n'

const { t, tn } = useI18n()

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
    if (n > 0) notice.value = tn(expose.value ? 'settings.categories.exposed' : 'settings.categories.protected', n)
  })
}

function startDelete(c: CategoryDto) {
  expose.value = false
  target.value = 'uncategorize'
  deleting.value = c
}

const confirmDelete = () => {
  const c = deleting.value!
  const tgt = target.value
  deleting.value = null
  return run(async () => {
    const dest = typeof tgt === 'number' ? cats.value.find((x) => x.id === tgt) : undefined
    const leaves = c.is_sensitive && !dest?.is_sensitive
    await removeCategory(c.id, typeof tgt === 'number' ? { moveTo: tgt } : { uncategorize: true }, leaves && expose.value)
  })
}
</script>

<template>
  <section class="card bg-base-200" data-testid="section-categories">
    <div class="card-body gap-3">
      <h2 class="card-title">{{ t('settings.categories.title') }}</h2>
      <p v-if="error" class="text-error" role="alert" data-testid="cat-error">{{ error }}</p>
      <p v-if="notice" class="text-info" data-testid="cat-notice">{{ notice }}</p>
      <ul class="flex flex-col gap-2">
        <li v-for="c in cats" :key="c.id" class="flex flex-wrap items-center gap-2" data-testid="cat-row">
          <template v-if="editingId === c.id">
            <input v-model="editName" class="input input-bordered input-sm" maxlength="64" data-testid="cat-edit-name" />
            <button class="btn btn-sm btn-primary" data-testid="cat-edit-save" @click="rename(c)">{{ t('common.save') }}</button>
            <button class="btn btn-sm btn-ghost" @click="editingId = null">{{ t('common.cancel') }}</button>
          </template>
          <template v-else>
            <span class="font-medium" data-testid="cat-name">{{ store.categoryLabel(c) }}</span>
            <span v-if="c.is_sensitive" class="badge badge-warning gap-1"><Icon icon="lucide:lock" aria-hidden="true" />{{ t('settings.categories.sensitive') }}</span>
            <span class="flex-1" />
            <button
              class="btn btn-sm btn-ghost" :disabled="store.masking && c.is_sensitive"
              :aria-label="t('settings.categories.renameAria', { name: store.categoryLabel(c) })" @click="startEdit(c)"
            >{{ t('settings.categories.rename') }}</button>
            <button class="btn btn-sm btn-ghost" data-testid="cat-toggle" @click="toggleSensitive(c)">
              {{ c.is_sensitive ? t('settings.categories.makeNotSensitive') : t('settings.categories.makeSensitive') }}
            </button>
            <button
              class="btn btn-sm btn-ghost text-error" data-testid="cat-delete"
              :aria-label="t('settings.categories.deleteAria', { name: store.categoryLabel(c) })" @click="startDelete(c)"
            ><Icon icon="lucide:trash-2" aria-hidden="true" /></button>
          </template>
        </li>
        <li v-if="!cats.length" class="opacity-70">{{ t('settings.categories.empty') }}</li>
      </ul>
      <form class="flex flex-wrap items-center gap-2" @submit.prevent="create">
        <input v-model="newName" class="input input-bordered input-sm" :placeholder="t('settings.categories.new')" maxlength="64" data-testid="cat-new-name" />
        <label class="label cursor-pointer gap-2">
          <input v-model="newSensitive" type="checkbox" class="checkbox checkbox-sm" data-testid="cat-new-sensitive" />
          <span class="label-text">{{ t('settings.categories.sensitive') }}</span>
        </label>
        <button class="btn btn-sm btn-primary" type="submit" data-testid="cat-create">{{ t('common.add') }}</button>
      </form>
    </div>

    <div v-if="unmarking" class="modal modal-open" role="dialog" aria-modal="true" data-testid="cat-unmark-dialog">
      <div class="modal-box">
        <h3 class="text-lg font-bold">{{ t('settings.categories.unmarkTitle') }}</h3>
        <p class="mt-2">{{ t('settings.categories.unmarkBody') }}</p>
        <label class="label cursor-pointer justify-start gap-2">
          <input v-model="expose" type="radio" class="radio" :value="false" data-testid="cat-freeze" />
          <span>{{ t('settings.categories.freeze') }}</span>
        </label>
        <label class="label cursor-pointer justify-start gap-2">
          <input v-model="expose" type="radio" class="radio" :value="true" data-testid="cat-expose" />
          <span>{{ t('settings.categories.expose') }}</span>
        </label>
        <div class="modal-action">
          <button class="btn" @click="unmarking = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" data-testid="cat-unmark-confirm" @click="confirmUnmark">{{ t('settings.categories.confirm') }}</button>
        </div>
      </div>
    </div>

    <div v-if="deleting" class="modal modal-open" role="dialog" aria-modal="true" data-testid="cat-delete-dialog">
      <div class="modal-box">
        <h3 class="text-lg font-bold">{{ t('settings.categories.deleteTitle', { name: store.categoryLabel(deleting) }) }}</h3>
        <p class="mt-2">{{ t('settings.categories.deleteBody') }}</p>
        <select v-model="target" class="select select-bordered mt-2 w-full" data-testid="cat-delete-target">
          <option value="uncategorize">{{ t('settings.categories.uncategorize') }}</option>
          <option v-for="c in cats.filter((x) => x.id !== deleting!.id)" :key="c.id" :value="c.id">
            {{ t('settings.categories.moveTo', { name: store.categoryLabel(c) }) }}
          </option>
        </select>
        <div v-if="deleting.is_sensitive" class="mt-3">
          <p>{{ t('settings.categories.deleteSensitive') }}</p>
          <label class="label cursor-pointer justify-start gap-2">
            <input v-model="expose" type="radio" class="radio" :value="false" />
            <span>{{ t('settings.categories.stayProtected') }}</span>
          </label>
          <label class="label cursor-pointer justify-start gap-2">
            <input v-model="expose" type="radio" class="radio" :value="true" data-testid="cat-delete-expose" />
            <span>{{ t('settings.categories.becomeExposed') }}</span>
          </label>
        </div>
        <div class="modal-action">
          <button class="btn" @click="deleting = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-error" data-testid="cat-delete-confirm" @click="confirmDelete">{{ t('common.delete') }}</button>
        </div>
      </div>
    </div>
  </section>
</template>
