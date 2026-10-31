import { apiClient } from './client'

export interface LeaderboardEntry {
  rank: number
  username: string
  total_tokens: number
  total_quota: string
}

export interface LeaderboardSnapshot {
  items: LeaderboardEntry[]
  models: string[]
  model: string
  updated_at: string
  limit: number
}

export async function getLeaderboard(model = '', signal?: AbortSignal): Promise<LeaderboardSnapshot> {
  return (await apiClient.get<LeaderboardSnapshot>('/leaderboard', { params: model ? { model } : {}, signal })).data
}
