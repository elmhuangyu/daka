<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Icon } from '@iconify/vue'
import {
  createCategory,
  describeError,
  listCategories,
  removeCategory,
  updateCategory,
  type CategoryDto,
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
    if (n > 0)
      notice.value = tn(
        expose.value ? 'settings.categories.exposed' : 'settings.categories.protected',
        n,
      )
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
    await removeCategory(
      c.id,
      typeof tgt === 'number' ? { moveTo: tgt } : { uncategorize: true },
      leaves && expose.value,
    )
  })
}
</script>

<template>
  <section
    class="card bg-base-200/50 border border-base-300 shadow-sm"
    data-testid="section-categories"
  >
    <div class="card-body p-6 gap-6">
      <div class="flex items-center gap-3 border-b border-base-300/80 pb-4">
        <div class="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Icon icon="lucide:tags" class="size-5" aria-hidden="true" />
        </div>
        <div>
          <h2 class="text-lg font-bold leading-none">{{ t('settings.categories.title') }}</h2>
        </div>
      </div>

      <div
        v-if="error"
        class="alert alert-error text-sm py-2.5 px-4 shadow-sm"
        role="alert"
        data-testid="cat-error"
      >
        <span>{{ error }}</span>
      </div>
      <div
        v-if="notice"
        class="alert alert-info text-sm py-2.5 px-4 shadow-sm"
        data-testid="cat-notice"
      >
        <span>{{ notice }}</span>
      </div>

      <ul class="flex flex-col gap-2">
        <li
          v-for="c in cats"
          :key="c.id"
          class="rounded-xl border border-base-300 bg-base-100/60 p-3 flex flex-wrap items-center justify-between gap-3 transition-colors hover:bg-base-100/90"
          data-testid="cat-row"
        >
          <template v-if="editingId === c.id">
            <div class="flex flex-1 items-center gap-2">
              <input
                v-model="editName"
                class="input input-bordered input-sm flex-1"
                maxlength="64"
                data-testid="cat-edit-name"
              />
              <button class="btn btn-sm btn-primary" data-testid="cat-edit-save" @click="rename(c)">
                {{ t('common.save') }}
              </button>
              <button class="btn btn-sm btn-ghost" @click="editingId = null">
                {{ t('common.cancel') }}
              </button>
            </div>
          </template>
          <template v-else>
            <div class="flex items-center gap-2.5 min-w-0">
              <span class="font-medium text-sm truncate" data-testid="cat-name">{{
                store.categoryLabel(c)
              }}</span>
              <span v-if="c.is_sensitive" class="badge badge-warning badge-sm gap-1">
                <Icon icon="lucide:lock" class="size-3" aria-hidden="true" />
                {{ t('settings.categories.sensitive') }}
              </span>
            </div>
            <div class="flex items-center gap-1 shrink-0">
              <button
                class="btn btn-sm btn-ghost gap-1"
                :disabled="store.masking && c.is_sensitive"
                :aria-label="t('settings.categories.renameAria', { name: store.categoryLabel(c) })"
                @click="startEdit(c)"
              >
                <Icon icon="lucide:pencil" class="size-3.5" aria-hidden="true" />
                <span class="hidden sm:inline text-xs">{{ t('settings.categories.rename') }}</span>
              </button>
              <button
                class="btn btn-sm btn-ghost text-xs"
                data-testid="cat-toggle"
                @click="toggleSensitive(c)"
              >
                {{
                  c.is_sensitive
                    ? t('settings.categories.makeNotSensitive')
                    : t('settings.categories.makeSensitive')
                }}
              </button>
              <button
                class="btn btn-sm btn-ghost text-error hover:bg-error/10"
                data-testid="cat-delete"
                :aria-label="t('settings.categories.deleteAria', { name: store.categoryLabel(c) })"
                @click="startDelete(c)"
              >
                <Icon icon="lucide:trash-2" class="size-4" aria-hidden="true" />
              </button>
            </div>
          </template>
        </li>
        <li
          v-if="!cats.length"
          class="rounded-xl border border-dashed border-base-300 p-8 text-center flex flex-col items-center justify-center gap-2"
        >
          <Icon icon="lucide:tags" class="size-8 opacity-25" aria-hidden="true" />
          <span class="text-sm opacity-70">{{ t('settings.categories.empty') }}</span>
        </li>
      </ul>

      <form
        class="rounded-xl border border-base-300 bg-base-100/40 p-3.5 flex flex-wrap items-center gap-3"
        @submit.prevent="create"
      >
        <div class="flex-1 min-w-44">
          <input
            v-model="newName"
            class="input input-bordered input-sm w-full"
            :placeholder="t('settings.categories.new')"
            maxlength="64"
            data-testid="cat-new-name"
          />
        </div>
        <label class="label cursor-pointer gap-2 py-0">
          <input
            v-model="newSensitive"
            type="checkbox"
            class="checkbox checkbox-sm checkbox-warning"
            data-testid="cat-new-sensitive"
          />
          <span class="label-text text-xs flex items-center gap-1">
            <Icon icon="lucide:lock" class="size-3.5 opacity-60" aria-hidden="true" />
            {{ t('settings.categories.sensitive') }}
          </span>
        </label>
        <button class="btn btn-sm btn-primary gap-1" type="submit" data-testid="cat-create">
          <Icon icon="lucide:plus" class="size-3.5" aria-hidden="true" />
          {{ t('common.add') }}
        </button>
      </form>
    </div>

    <div
      v-if="unmarking"
      class="modal modal-open"
      role="dialog"
      aria-modal="true"
      data-testid="cat-unmark-dialog"
    >
      <div class="modal-box">
        <h3 class="text-lg font-bold">{{ t('settings.categories.unmarkTitle') }}</h3>
        <p class="mt-2 text-sm opacity-80">{{ t('settings.categories.unmarkBody') }}</p>
        <div class="mt-4 flex flex-col gap-2">
          <label
            class="label cursor-pointer justify-start gap-3 rounded-lg border border-base-300 p-3 bg-base-100/50"
          >
            <input
              v-model="expose"
              type="radio"
              class="radio radio-primary radio-sm"
              :value="false"
              data-testid="cat-freeze"
            />
            <span class="text-sm">{{ t('settings.categories.freeze') }}</span>
          </label>
          <label
            class="label cursor-pointer justify-start gap-3 rounded-lg border border-base-300 p-3 bg-base-100/50"
          >
            <input
              v-model="expose"
              type="radio"
              class="radio radio-primary radio-sm"
              :value="true"
              data-testid="cat-expose"
            />
            <span class="text-sm">{{ t('settings.categories.expose') }}</span>
          </label>
        </div>
        <div class="modal-action">
          <button class="btn btn-sm" @click="unmarking = null">{{ t('common.cancel') }}</button>
          <button
            class="btn btn-sm btn-primary"
            data-testid="cat-unmark-confirm"
            @click="confirmUnmark"
          >
            {{ t('settings.categories.confirm') }}
          </button>
        </div>
      </div>
    </div>

    <div
      v-if="deleting"
      class="modal modal-open"
      role="dialog"
      aria-modal="true"
      data-testid="cat-delete-dialog"
    >
      <div class="modal-box">
        <h3 class="text-lg font-bold">
          {{ t('settings.categories.deleteTitle', { name: store.categoryLabel(deleting) }) }}
        </h3>
        <p class="mt-2 text-sm opacity-80">{{ t('settings.categories.deleteBody') }}</p>
        <select
          v-model="target"
          class="select select-bordered select-sm mt-3 w-full"
          data-testid="cat-delete-target"
        >
          <option value="uncategorize">{{ t('settings.categories.uncategorize') }}</option>
          <option v-for="c in cats.filter((x) => x.id !== deleting!.id)" :key="c.id" :value="c.id">
            {{ t('settings.categories.moveTo', { name: store.categoryLabel(c) }) }}
          </option>
        </select>
        <div
          v-if="deleting.is_sensitive"
          class="mt-4 p-3 rounded-lg border border-warning/30 bg-warning/5"
        >
          <p class="text-xs font-medium text-warning">
            {{ t('settings.categories.deleteSensitive') }}
          </p>
          <div class="mt-2 flex flex-col gap-1.5">
            <label class="label cursor-pointer justify-start gap-2 py-0">
              <input
                v-model="expose"
                type="radio"
                class="radio radio-warning radio-sm"
                :value="false"
              />
              <span class="text-xs">{{ t('settings.categories.stayProtected') }}</span>
            </label>
            <label class="label cursor-pointer justify-start gap-2 py-0">
              <input
                v-model="expose"
                type="radio"
                class="radio radio-warning radio-sm"
                :value="true"
                data-testid="cat-delete-expose"
              />
              <span class="text-xs">{{ t('settings.categories.becomeExposed') }}</span>
            </label>
          </div>
        </div>
        <div class="modal-action">
          <button class="btn btn-sm" @click="deleting = null">{{ t('common.cancel') }}</button>
          <button
            class="btn btn-sm btn-error"
            data-testid="cat-delete-confirm"
            @click="confirmDelete"
          >
            {{ t('common.delete') }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
