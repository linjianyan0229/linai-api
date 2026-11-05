import type { GroupRechargeRateTier } from '@/types'

export function defaultGroupRechargeRateTiers(): GroupRechargeRateTier[] {
  return [
    { min_amount: 0, rate_multiplier: 1 },
    { min_amount: 100, rate_multiplier: 0.75 },
    { min_amount: 500, rate_multiplier: 0.5 }
  ]
}

export function validGroupRechargeRateTiers(tiers: GroupRechargeRateTier[]): boolean {
  if (tiers.length === 0) return true
  if (tiers.length !== 3 || tiers[0]?.min_amount !== 0) return false
  const precision = (value: number, digits: number) => Math.abs(value - Number(value.toFixed(digits))) < 1e-9
  return tiers.every((tier, index) =>
    Number.isFinite(tier.min_amount) && tier.min_amount >= 0 && tier.min_amount <= 1e12 && precision(tier.min_amount, 2) &&
    Number.isFinite(tier.rate_multiplier) && tier.rate_multiplier >= 0.5 && tier.rate_multiplier <= 1 && precision(tier.rate_multiplier, 4) &&
    (index === 0 || (tier.min_amount > tiers[index - 1]!.min_amount && tier.rate_multiplier < tiers[index - 1]!.rate_multiplier))
  )
}
