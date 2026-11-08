export interface GroupDynamicRateFields {
  dynamic_rate_enabled?: boolean
  dynamic_rate_min?: number | null
  dynamic_rate_max?: number | null
}

export function dynamicRateRange(fields: GroupDynamicRateFields): string | null {
  const min = fields.dynamic_rate_min, max = fields.dynamic_rate_max
  if (min == null || max == null || !Number.isFinite(min) || !Number.isFinite(max) || min < 0 || max < min) return null
  const format = (rate: number) => Number(rate.toPrecision(10)).toString()
  return min === max ? `${format(min)}×` : `${format(min)}–${format(max)}×`
}

export function validDynamicRateFactor(factor: number): boolean {
  return Number.isFinite(factor) && factor > 0 && factor <= 9999.9999 && Math.abs(factor * 1e4 - Math.round(factor * 1e4)) < 1e-6
}
