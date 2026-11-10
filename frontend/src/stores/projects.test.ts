import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import * as api from '../api'
import type { Project } from '../api'
import { usePrivacyStore } from './privacy'
import { useProjectsStore } from './projects'

function sampleProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 1,
    category_id: null,
    name: 'Test Project',
    description: 'Description',
    color: '#3b82f6',
    status: 'active',
    target_date: null,
    is_sensitive: false,
    sort_order: 0,
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
    ...overrides,
  }
}

describe('projects store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.restoreAllMocks()
  })

  it('computes activeProjects, completedProjects, archivedProjects, and projectById correctly', () => {
    const store = useProjectsStore()
    const p1 = sampleProject({ id: 1, name: 'Active B', status: 'active', sort_order: 10 })
    const p2 = sampleProject({ id: 2, name: 'Active A', status: 'active', sort_order: 5 })
    const p3 = sampleProject({ id: 3, name: 'Active C', status: 'active', sort_order: 10 })
    const p4 = sampleProject({ id: 4, name: 'Completed 1', status: 'completed', sort_order: 1 })
    const p5 = sampleProject({ id: 5, name: 'Archived 1', status: 'archived', sort_order: 1 })

    store.projects = [p1, p2, p3, p4, p5]

    // activeProjects sorted by sort_order asc, id asc: p2 (5), p1 (10, id 1), p3 (10, id 3)
    expect(store.activeProjects).toEqual([p2, p1, p3])
    expect(store.completedProjects).toEqual([p4])
    expect(store.archivedProjects).toEqual([p5])

    expect(store.projectById.get(1)).toEqual(p1)
    expect(store.projectById.get(2)).toEqual(p2)
    expect(store.projectById.get(999)).toBeUndefined()
  })

  it('fetchProjects calls api.listProjects and handles loading & errors', async () => {
    const store = useProjectsStore()
    const privacy = usePrivacyStore()
    privacy.set(false)

    const listSpy = vi.spyOn(api, 'listProjects')
    const projs = [sampleProject({ id: 10, name: 'Project 10' })]
    listSpy.mockResolvedValueOnce(projs)

    const fetchPromise = store.fetchProjects({ status: 'active' })
    expect(store.loading).toBe(true)
    const res = await fetchPromise
    expect(store.loading).toBe(false)
    expect(res).toEqual(projs)
    expect(store.projects).toEqual(projs)
    expect(listSpy).toHaveBeenCalledWith({ status: 'active', redact: false })

    // Error case
    listSpy.mockRejectedValueOnce(new api.ApiError('Server error', 500))
    await store.fetchProjects()
    expect(store.loading).toBe(false)
    expect(store.error).toBeTruthy()
  })

  it('createProject calls api.createProject and appends to store', async () => {
    const store = useProjectsStore()
    const newProj = sampleProject({ id: 20, name: 'New Project' })
    const createSpy = vi.spyOn(api, 'createProject').mockResolvedValueOnce(newProj)

    const created = await store.createProject({ name: 'New Project' })
    expect(createSpy).toHaveBeenCalledWith({ name: 'New Project' })
    expect(created).toEqual(newProj)
    expect(store.projects).toContainEqual(newProj)
  })

  it('updateProject calls api.updateProject and updates in store', async () => {
    const store = useProjectsStore()
    const original = sampleProject({ id: 30, name: 'Old Name' })
    store.projects = [original]

    const updated = sampleProject({ id: 30, name: 'New Name' })
    const updateSpy = vi.spyOn(api, 'updateProject').mockResolvedValueOnce(updated)

    const res = await store.updateProject(30, { name: 'New Name' }, true)
    expect(updateSpy).toHaveBeenCalledWith(30, { name: 'New Name' }, true)
    expect(res).toEqual(updated)
    expect(store.projects).toEqual([updated])
  })

  it('deleteProject calls api.deleteProject and removes from store', async () => {
    const store = useProjectsStore()
    const p1 = sampleProject({ id: 40, name: 'To Delete' })
    const p2 = sampleProject({ id: 41, name: 'To Keep' })
    store.projects = [p1, p2]

    const deleteSpy = vi.spyOn(api, 'deleteProject').mockResolvedValueOnce({ deleted: true })

    const res = await store.deleteProject(40, { cascade: true })
    expect(deleteSpy).toHaveBeenCalledWith(40, { cascade: true })
    expect(res).toEqual({ deleted: true })
    expect(store.projects).toEqual([p2])
  })

  it('privacy guard: clears plaintext projects synchronously when privacy is turned on', () => {
    const store = useProjectsStore()
    const privacy = usePrivacyStore()
    privacy.set(false)

    store.projects = [sampleProject({ id: 50, name: 'Confidential Project', is_sensitive: true })]
    expect(store.projects).toHaveLength(1)

    // Turning privacy on drops plaintext synchronously
    privacy.set(true)
    expect(store.projects).toHaveLength(0)
  })

  it('when privacy.redact is true, createProject fetches redacted projects and does not pollute projects.value', async () => {
    const store = useProjectsStore()
    const privacy = usePrivacyStore()
    privacy.set(true)

    const unredactedNew = sampleProject({
      id: 60,
      name: 'Unredacted Secret Name',
      description: 'Secret Details',
      is_sensitive: true,
    })
    const redactedList = [
      sampleProject({
        id: 60,
        name: 'Private project',
        description: '',
        is_sensitive: true,
      }),
    ]

    const createSpy = vi.spyOn(api, 'createProject').mockResolvedValueOnce(unredactedNew)
    const listSpy = vi.spyOn(api, 'listProjects').mockResolvedValueOnce(redactedList)

    const created = await store.createProject({
      name: 'Unredacted Secret Name',
      is_sensitive: true,
    })

    expect(createSpy).toHaveBeenCalledWith({ name: 'Unredacted Secret Name', is_sensitive: true })
    expect(created).toEqual(unredactedNew)
    expect(listSpy).toHaveBeenCalledWith({ redact: true })
    expect(store.projects).toEqual(redactedList)
    expect(store.projects.some((p) => p.name === 'Unredacted Secret Name')).toBe(false)
  })

  it('when privacy.redact is true, updateProject fetches redacted projects and does not pollute projects.value', async () => {
    const store = useProjectsStore()
    const privacy = usePrivacyStore()
    privacy.set(true)

    const unredactedUpdated = sampleProject({
      id: 70,
      name: 'Unredacted Updated Name',
      description: 'Updated Secret',
      is_sensitive: true,
    })
    const redactedList = [
      sampleProject({
        id: 70,
        name: 'Private project',
        description: '',
        is_sensitive: true,
      }),
    ]

    const updateSpy = vi.spyOn(api, 'updateProject').mockResolvedValueOnce(unredactedUpdated)
    const listSpy = vi.spyOn(api, 'listProjects').mockResolvedValueOnce(redactedList)

    const updated = await store.updateProject(70, { name: 'Unredacted Updated Name' })

    expect(updateSpy).toHaveBeenCalledWith(70, { name: 'Unredacted Updated Name' }, undefined)
    expect(updated).toEqual(unredactedUpdated)
    expect(listSpy).toHaveBeenCalledWith({ redact: true })
    expect(store.projects).toEqual(redactedList)
    expect(store.projects.some((p) => p.name === 'Unredacted Updated Name')).toBe(false)
  })
})
