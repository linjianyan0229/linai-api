import { describe, expect, it } from 'vitest'
import { addGroupTimeRate, defaultGroupTimeRates, formatGroupTimeRates, removeGroupTimeRate, timeAdjustedRate, validGroupTimeRates } from '../groupTimeRates'

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
  it('uses Asia/Shanghai and switches at the exact boundary in both pricing modes', () => {
    const fields = { time_rate_periods: periods }
    expect(timeAdjustedRate(fields, 0.8, new Date('2026-11-06T11:59:59Z'))).toBe(1.2)
    expect(timeAdjustedRate(fields, 0.8, new Date('2026-11-06T12:00:00Z'))).toBe(0.6)
    expect(timeAdjustedRate({ ...fields, recharge_rate_enabled: true }, 0.75, new Date('2026-11-06T11:59:59Z'))).toBe(0.9)
    expect(timeAdjustedRate({ ...fields, recharge_rate_enabled: true }, 0.75, new Date('2026-11-06T12:00:00Z'))).toBe(0.45)
    expect(timeAdjustedRate({}, 0.8, new Date())).toBe(0.8)
  })
  it('adds several peak and off-peak windows, preserves coverage, and merges on removal', () => {
    const original = defaultGroupTimeRates(0.8)
    const four = addGroupTimeRate(original, 'peak')!
    expect(four).toHaveLength(4)
    expect(validGroupTimeRates(four)).toBe(true)
    expect(four.filter(p => p.kind === 'peak')).toHaveLength(2)
    expect(four.filter(p => p.kind === 'valley')).toHaveLength(2)
    const six = addGroupTimeRate(four, 'valley')!
    expect(six).toHaveLength(6)
    expect(validGroupTimeRates(six)).toBe(true)
    const five = removeGroupTimeRate(six, 2)!
    expect(five).toHaveLength(5)
    expect(validGroupTimeRates(five)).toBe(true)
    expect(original).toEqual(defaultGroupTimeRates(0.8))
    expect(removeGroupTimeRate(original, 0)).toBeNull()
    let many = six
    while (many.length < 48) many = addGroupTimeRate(many, 'peak')!
    expect(validGroupTimeRates(many)).toBe(true)
    expect(addGroupTimeRate(many, 'valley')).toBeNull()
    expect(validGroupTimeRates([...many, many[0]!])).toBe(false)
  })
  it('supports four daily windows and reports only Shanghai time', () => {
    const periods = [
      {start:'08:00',end:'12:00',rate_multiplier:1.2},
      {start:'12:00',end:'18:00',rate_multiplier:0.6},
      {start:'18:00',end:'22:00',rate_multiplier:1.1},
      {start:'22:00',end:'08:00',rate_multiplier:0.5},
    ]
    expect(validGroupTimeRates(periods)).toBe(true)
    for (const [utc, rate] of [[0,1.2],[4,0.6],[10,1.1],[14,0.5]]) {
      expect(timeAdjustedRate({time_rate_periods:periods},0.8,new Date(Date.UTC(2026,10,6,utc!)))).toBe(rate)
    }
    expect(formatGroupTimeRates({time_rate_periods:periods})).toContain('(Asia/Shanghai)')
    expect(validGroupTimeRates(periods.map((p,i)=>i===1?{...p,start:'13:00'}:p))).toBe(false)
    expect(validGroupTimeRates(periods.map((p,i)=>i===1?{...p,start:'11:00'}:p))).toBe(false)
  })

})
