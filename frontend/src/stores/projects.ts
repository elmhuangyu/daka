import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import * as api from '../api'
import type { CreateProjectRequest, Project, ProjectStatus, UpdateProjectRequest } from '../api'
import { usePrivacyStore } from './privacy'

/**
 * Projects store state and actions.
 * Plaintext is dropped synchronously when the privacy guard turns on.
 */
export const useProjectsStore = defineStore('projects', () => {
  const privacy = usePrivacyStore()

  const projects = ref<Project[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  let seq = 0

  function clear() {
    seq++
    projects.value = []
    loading.value = false
    error.value = null
  }

  watch(
    () => privacy.redact,
    (on) => {
      if (on) clear()
    },
    { flush: 'sync' },
  )

  const activeProjects = computed(() =>
    projects.value
      .filter((p) => p.status === 'active')
      .slice()
      .sort((a, b) => a.sort_order - b.sort_order || a.id - b.id),
  )

  const completedProjects = computed(() =>
    projects.value
      .filter((p) => p.status === 'completed')
      .slice()
      .sort((a, b) => a.sort_order - b.sort_order || a.id - b.id),
  )

  const archivedProjects = computed(() =>
    projects.value
      .filter((p) => p.status === 'archived')
      .slice()
      .sort((a, b) => a.sort_order - b.sort_order || a.id - b.id),
  )

  const projectById = computed(() => {
    const map = new Map<number, Project>()
    for (const p of projects.value) {
      map.set(p.id, p)
    }
    return map
  })

  async function fetchProjects(params?: { status?: ProjectStatus; category_id?: number }) {
    const my = ++seq
    loading.value = true
    error.value = null
    try {
      const data = await api.listProjects({
        ...params,
        redact: privacy.redact,
      })
      if (my !== seq) return
      projects.value = Array.isArray(data) ? data : []
      error.value = null
      return data
    } catch (e) {
      if (my !== seq) return
      error.value = api.describeError(e)
    } finally {
      if (my === seq) loading.value = false
    }
  }

  async function createProject(req: CreateProjectRequest) {
    loading.value = true
    error.value = null
    try {
      const created = await api.createProject(req)
      if (privacy.redact) {
        await fetchProjects()
        return created
      }
      projects.value.push(created)
      return created
    } catch (e) {
      error.value = api.describeError(e)
      throw e
    } finally {
      loading.value = false
    }
  }

  async function updateProject(id: number, req: UpdateProjectRequest, confirmExposure?: boolean) {
    loading.value = true
    error.value = null
    try {
      const updated = await api.updateProject(id, req, confirmExposure)
      if (privacy.redact) {
        await fetchProjects()
        return updated
      }
      const idx = projects.value.findIndex((p) => p.id === id)
      if (idx >= 0) {
        projects.value[idx] = updated
      } else {
        projects.value.push(updated)
      }
      return updated
    } catch (e) {
      error.value = api.describeError(e)
      throw e
    } finally {
      loading.value = false
    }
  }

  async function deleteProject(
    id: number,
    options?: { cascade?: boolean; unlink?: boolean; confirmExposure?: boolean },
  ) {
    loading.value = true
    error.value = null
    try {
      const res = await api.deleteProject(id, options)
      projects.value = projects.value.filter((p) => p.id !== id)
      return res
    } catch (e) {
      error.value = api.describeError(e)
      throw e
    } finally {
      loading.value = false
    }
  }

  return {
    projects,
    loading,
    error,
    activeProjects,
    completedProjects,
    archivedProjects,
    projectById,
    fetchProjects,
    createProject,
    updateProject,
    deleteProject,
    clear,
  }
})
