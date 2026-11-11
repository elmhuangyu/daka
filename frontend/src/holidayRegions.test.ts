import { describe, expect, it } from 'vitest'
import { SUBDIVISIONS, splitRegion } from './holidayRegions'

describe('holidayRegions', () => {
  it('splits regions', () => {
    expect(splitRegion('CA-ON')).toEqual(['CA', 'CA-ON'])
    expect(splitRegion('CN')).toEqual(['CN', ''])
    expect(splitRegion('')).toEqual(['', ''])
  })
  it('lists subdivisions for CA and US only', () => {
    expect(SUBDIVISIONS.CA).toHaveLength(13)
    expect(SUBDIVISIONS.US).toHaveLength(51)
    expect(SUBDIVISIONS.CN).toBeUndefined()
  })
})
