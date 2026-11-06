import { useNow } from '@vueuse/core'
import { useAppStore } from '@/stores/app'
import { timeAdjustedRate, type GroupTimeRateFields } from '@/utils/groupTimeRates'

export function useGroupTimeRate() {
  const now = useNow({ interval: 1000 })
  const appStore = useAppStore()
  return (fields: GroupTimeRateFields, base: number) => timeAdjustedRate(fields, base, now.value, appStore.cachedPublicSettings?.server_timezone || 'Asia/Shanghai')
}
