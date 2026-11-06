/**
 * User Groups API endpoints (non-admin)
 * Handles group-related operations for regular users
 */

import { apiClient } from './client'
import type { Group, GroupRechargeRateTier, GroupTimeRatePeriod } from '@/types'

/**
 * Get available groups that the current user can bind to API keys
 * This returns groups based on user's permissions:
 * - Standard groups: public (non-exclusive) or explicitly allowed
 * - Subscription groups: user has active subscription
 * @returns List of available groups
 */
export async function getAvailable(): Promise<Group[]> {
  const { data } = await apiClient.get<Group[]>('/groups/available')
  return data
}

/**
 * Get current user's custom group rate multipliers
 * @returns Map of group_id to custom rate_multiplier
 */
export async function getUserGroupRates(): Promise<Record<number, number>> {
  const { data } = await apiClient.get<Record<number, number> | null>('/groups/rates')
  return data || {}
}

export interface GroupRechargeProgress {
  time_rate_periods?: GroupTimeRatePeriod[]
  group_id: number
  group_name: string
  tiers: GroupRechargeRateTier[]
  level: number
  rate_multiplier: number
  has_user_override: boolean
}

export interface UserRechargeProgress {
  principal_usd: number
  groups: GroupRechargeProgress[]
}

export async function getRechargeProgress(): Promise<UserRechargeProgress> {
  const { data } = await apiClient.get<UserRechargeProgress>('/groups/recharge-progress')
  return data
}

export const userGroupsAPI = {
  getRechargeProgress,
  getAvailable,
  getUserGroupRates
}

export default userGroupsAPI
