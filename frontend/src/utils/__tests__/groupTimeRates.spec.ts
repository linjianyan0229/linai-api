import { describe, expect, it } from 'vitest'
import { defaultGroupTimeRates, timeAdjustedRate, validGroupTimeRates } from '../groupTimeRates'

describe('group time rates', () => {
  const periods = [{ start: '08:00', end: '20:00', rate_multiplier: 1.2 }, { start: '20:00', end: '08:00', rate_multiplier: 0.6 }]
  it('validates 24-hour coverage, distinct boundaries, and numeric precision', () => {
    expect(validGroupTimeRates([])).toBe(true)
    expect(validGroupTimeRates(periods)).toBe(true)
    expect(validGroupTimeRates([periods[0]!])).toBe(false)
    expect(validGroupTimeRates([periods[0]!, { ...periods[1]!, start: '21:00' }])).toBe(false)
    expect(validGroupTimeRates([{ ...periods[0]!, rate_multiplier: 0.12345 }, periods[1]!])).toBe(false)
    expect(validGroupTimeRates(defaultGroupTimeRates(0))).toBe(true)
  })
  it('uses server time and switches at the exact boundary in both pricing modes', () => {
    const fields = { time_rate_periods: periods }
    expect(timeAdjustedRate(fields, 0.8, new Date('2026-11-06T11:59:59Z'), 'Asia/Shanghai')).toBe(1.2)
    expect(timeAdjustedRate(fields, 0.8, new Date('2026-11-06T12:00:00Z'), 'Asia/Shanghai')).toBe(0.6)
    expect(timeAdjustedRate({ ...fields, recharge_rate_enabled: true }, 0.75, new Date('2026-11-06T11:59:59Z'), 'Asia/Shanghai')).toBe(0.9)
    expect(timeAdjustedRate({ ...fields, recharge_rate_enabled: true }, 0.75, new Date('2026-11-06T12:00:00Z'), 'Asia/Shanghai')).toBe(0.45)
    expect(timeAdjustedRate({}, 0.8, new Date(), 'UTC')).toBe(0.8)
  })
})
