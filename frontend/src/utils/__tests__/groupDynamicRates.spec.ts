import { describe, expect, it } from 'vitest'
import { dynamicRateRange, validDynamicRateFactor } from '../groupDynamicRates'
import { timeAdjustedRate } from '../groupTimeRates'

describe('dynamic group rates', () => {
  it('shows final prices and preserves explicitly free and tiny positive quotes', () => {
    expect(dynamicRateRange({ dynamic_rate_min: .16, dynamic_rate_max: .2 })).toBe('0.16–0.2×')
    expect(dynamicRateRange({ dynamic_rate_min: 0, dynamic_rate_max: 0 })).toBe('0×')
    expect(dynamicRateRange({ dynamic_rate_min: 1e-8, dynamic_rate_max: 1e-8 })).toBe('1e-8×')
    expect(dynamicRateRange({})).toBeNull()
    expect(dynamicRateRange({ dynamic_rate_min: .2, dynamic_rate_max: .1 })).toBeNull()
  })
  it('uses a quoted upper bound rather than dormant user/recharge/time prices', () => {
    expect(timeAdjustedRate({ dynamic_rate_enabled: true, dynamic_rate_max: .16, time_rate_periods: [{ start: '00:00', end: '12:00', rate_multiplier: 3 }, { start: '12:00', end: '00:00', rate_multiplier: 2 }] }, .5, new Date())).toBe(.16)
  })
  it('validates the same factor precision and range as the API', () => {
    for (const factor of [0, -1, NaN, Infinity, 10000, 1.12345]) expect(validDynamicRateFactor(factor)).toBe(false)
    for (const factor of [.0001, 2, 9999.9999]) expect(validDynamicRateFactor(factor)).toBe(true)
  })
})
