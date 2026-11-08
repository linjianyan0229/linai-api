import type { GroupTimeRatePeriod } from '@/types'

export const GROUP_TIME_RATE_TIMEZONE = 'Asia/Shanghai'
export const MAX_GROUP_TIME_RATE_PERIODS = 48
export type GroupTimeRateKind = 'peak' | 'valley'

export interface GroupTimeRateFields {
  dynamic_rate_enabled?: boolean
  dynamic_rate_max?: number | null
  time_rate_periods?: GroupTimeRatePeriod[]
  recharge_rate_enabled?: boolean
  recharge_rate_tiers?: unknown[]
}

function minutes(value: string): number | null {
  const match = /^(\d{1,2}):([0-5]\d)$/.exec(value)
  if (!match || Number(match[1]) > 23) return null
  return Number(match[1]) * 60 + Number(match[2])
}

function clock(value: number): string {
  const minute = (value + 1440) % 1440
  return `${String(Math.floor(minute / 60)).padStart(2, '0')}:${String(minute % 60).padStart(2, '0')}`
}

export function groupTimeRateKind(period: GroupTimeRatePeriod, index: number): GroupTimeRateKind {
  return period.kind ?? (index % 2 === 0 ? 'peak' : 'valley')
}

export function normalizeGroupTimeRates(periods: GroupTimeRatePeriod[]): GroupTimeRatePeriod[] {
  return periods.map((p, i) => ({ ...p, kind: groupTimeRateKind(p, i) }))
}

export function validGroupTimeRates(periods: GroupTimeRatePeriod[]): boolean {
  if (!periods.length) return true
  if (periods.length < 2 || periods.length > MAX_GROUP_TIME_RATE_PERIODS) return false
  const spans: [number, number][] = []
  for (const p of periods) {
    const start = minutes(p.start), end = minutes(p.end)
    if (start === null || end === null || start === end ||
      (p.kind !== undefined && p.kind !== 'peak' && p.kind !== 'valley') ||
      !Number.isFinite(p.rate_multiplier) || p.rate_multiplier < 0 || p.rate_multiplier > 999999.9999 ||
      Math.abs(p.rate_multiplier - Number(p.rate_multiplier.toFixed(4))) >= 1e-9) return false
    if (start < end) spans.push([start, end])
    else {
      spans.push([start, 1440])
      if (end > 0) spans.push([0, end])
    }
  }
  spans.sort((a, b) => a[0] - b[0])
  let cursor = 0
  for (const [start, end] of spans) {
    if (start !== cursor) return false
    cursor = end
  }
  return cursor === 1440
}

export function defaultGroupTimeRates(rate = 1): GroupTimeRatePeriod[] {
  return [{ kind: 'peak', start: '08:00', end: '20:00', rate_multiplier: rate },
    { kind: 'valley', start: '20:00', end: '08:00', rate_multiplier: rate }]
}

// Insert a new window inside the longest opposite window, retaining full coverage.
export function addGroupTimeRate(input: GroupTimeRatePeriod[], kind: GroupTimeRateKind): GroupTimeRatePeriod[] | null {
  if (!input.length || !validGroupTimeRates(input)) return null
  const periods = normalizeGroupTimeRates(input)
  const candidates = periods.map((p, index) => ({ p, index, start: minutes(p.start)!,
    duration: (minutes(p.end)! - minutes(p.start)! + 1440) % 1440 }))
    .sort((a, b) => b.duration - a.duration)
  const opposite = candidates.find(c => c.p.kind !== kind && c.duration >= 3)
  if (opposite && periods.length + 2 <= MAX_GROUP_TIME_RATE_PERIODS) {
    const { p, index, start, duration } = opposite
    const first = clock(start + Math.floor(duration / 3)), second = clock(start + Math.floor(2 * duration / 3))
    periods[index] = { ...p, end: first }
    const rate = periods.find(existing => existing.kind === kind)?.rate_multiplier ?? p.rate_multiplier
    return [...periods, { kind, start: first, end: second, rate_multiplier: rate }, { ...p, start: second }]
  }
  const same = candidates.find(c => c.p.kind === kind && c.duration >= 2)
  if (!same || periods.length >= MAX_GROUP_TIME_RATE_PERIODS) return null
  const midpoint = clock(same.start + Math.floor(same.duration / 2))
  periods[same.index] = { ...same.p, end: midpoint }
  return [...periods, { ...same.p, start: midpoint }]
}

export function removeGroupTimeRate(input: GroupTimeRatePeriod[], index: number): GroupTimeRatePeriod[] | null {
  if (input.length <= 2 || !validGroupTimeRates(input) || !input[index]) return null
  const periods = normalizeGroupTimeRates(input), removed = periods[index]!
  const previous = periods.find((p, i) => i !== index && minutes(p.end) === minutes(removed.start))
  if (!previous) return null
  previous.end = removed.end
  return periods.filter((_, i) => i !== index)
}

export function formatGroupTimeRates(fields: GroupTimeRateFields): string {
  if (!fields.time_rate_periods?.length) return ''
  return [...fields.time_rate_periods].sort((a, b) => (minutes(a.start) ?? 0) - (minutes(b.start) ?? 0))
    .map(p => `${p.start}–${p.end} ×${p.rate_multiplier}`).join(' / ') + ` (${GROUP_TIME_RATE_TIMEZONE})`
}

const shanghaiClock = new Intl.DateTimeFormat('en-GB', { timeZone: GROUP_TIME_RATE_TIMEZONE, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })

export function timeAdjustedRate(fields: GroupTimeRateFields, base: number, now: Date): number {
  if (fields.dynamic_rate_enabled) return fields.dynamic_rate_max ?? base
  if (!fields.time_rate_periods?.length) return base
  const parts = shanghaiClock.formatToParts(now)
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
