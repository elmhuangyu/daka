import { describe, expect, it } from 'vitest'
import { groupByTree } from './logic'

describe('groupByTree', () => {
  const parents: Record<number, number | null> = { 1: null, 2: 1, 3: 2, 4: null, 5: null }
  const proj: Record<number, number | null> = { 1: 7, 2: 7, 3: 7, 4: null, 5: 7 }

  it('puts grandchildren under their ancestors and keeps the project together', () => {
    const out = groupByTree(
      [{ id: 3 }, { id: 4 }, { id: 1 }, { id: 5 }, { id: 2 }],
      (e) => ({ id: e.id, projectId: proj[e.id] ?? null }),
      (id) => parents[id] ?? null,
    )
    expect(out.map((e) => [e.id, e.depth])).toEqual([
      [4, 0],
      [1, 0],
      [2, 1],
      [3, 2],
      [5, 0],
    ])
  })

  it('finds a grandparent when the parent is not in the list', () => {
    const out = groupByTree(
      [{ id: 1 }, { id: 4 }, { id: 3 }],
      (e) => ({ id: e.id, projectId: proj[e.id] ?? null }),
      (id) => parents[id] ?? null,
    )
    expect(out.map((e) => [e.id, e.depth])).toEqual([
      [1, 0],
      [3, 1],
      [4, 0],
    ])
  })
})
