import { useNow } from '@vueuse/core'
import { timeAdjustedRate, type GroupTimeRateFields } from '@/utils/groupTimeRates'

export function useGroupTimeRate() {
  const now = useNow({ interval: 1000 })
  return (fields: GroupTimeRateFields, base: number) => timeAdjustedRate(fields, base, now.value)
}
