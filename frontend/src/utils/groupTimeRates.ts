import type { GroupTimeRatePeriod } from '@/types'

export interface GroupTimeRateFields {
  time_rate_periods?: GroupTimeRatePeriod[]
  recharge_rate_enabled?: boolean
  recharge_rate_tiers?: unknown[]
}

function minutes(value: string): number | null {
  const match = /^(\d{1,2}):([0-5]\d)$/.exec(value)
  if (!match || Number(match[1]) > 23) return null
  return Number(match[1]) * 60 + Number(match[2])
}

export function validGroupTimeRates(periods: GroupTimeRatePeriod[]): boolean {
  if (!periods.length) return true
  if (periods.length !== 2) return false
  const [a, b] = periods as [GroupTimeRatePeriod, GroupTimeRatePeriod]
  return periods.every(p => minutes(p.start) !== null && minutes(p.end) !== null &&
    minutes(p.start) !== minutes(p.end) && Number.isFinite(p.rate_multiplier) &&
    p.rate_multiplier >= 0 && p.rate_multiplier <= 999999.9999 &&
    Math.abs(p.rate_multiplier - Number(p.rate_multiplier.toFixed(4))) < 1e-9) &&
    minutes(a.start) === minutes(b.end) && minutes(a.end) === minutes(b.start)
}

export function defaultGroupTimeRates(rate = 1): GroupTimeRatePeriod[] {
  return [{ start: '08:00', end: '20:00', rate_multiplier: rate },
    { start: '20:00', end: '08:00', rate_multiplier: rate }]
}

export function formatGroupTimeRates(fields: GroupTimeRateFields, timezone: string): string {
  if (fields.time_rate_periods?.length !== 2) return ''
  return fields.time_rate_periods.map(p => `${p.start}–${p.end} ×${p.rate_multiplier}`).join(' / ') + ` (${timezone})`
}

export function timeAdjustedRate(fields: GroupTimeRateFields, base: number, now: Date, timezone: string): number {
  if (fields.time_rate_periods?.length !== 2) return base
  const parts = new Intl.DateTimeFormat('en-GB', { timeZone: timezone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(now)
  const minute = Number(parts.find(p => p.type === 'hour')?.value) * 60 + Number(parts.find(p => p.type === 'minute')?.value)
  const period = fields.time_rate_periods.find(p => {
    const start = minutes(p.start), end = minutes(p.end)
    return start !== null && end !== null && (start < end ? minute >= start && minute < end : minute >= start || minute < end)
  })
  if (!period) return base
  return fields.recharge_rate_enabled || fields.recharge_rate_tiers?.length === 3
    ? Math.round(base * period.rate_multiplier * 1e8) / 1e8
    : period.rate_multiplier
}
