/** Local build information and opt-in updates from the configured releases. */
import { apiClient } from '../client'

export async function getVersion(): Promise<{ version: string }> {
  const { data } = await apiClient.get<{ version: string }>('/admin/system/version', { timeout: 5000 })
  return data
}

export async function restartService(): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>('/admin/system/restart', undefined, { headers: { 'Idempotency-Key': createSystemOperationKey('restart') } })
  return data
}

export interface AvailableRelease {
  version: string
  name: string
  notes: string
  published_at: string
  url: string
  can_install: boolean
}

export interface ReleaseUpdateInfo {
  current_version: string
  latest_version: string
  repository: string
  has_update: boolean
  can_update: boolean
  unsupported_reason?: string
  deployment: 'container' | 'binary'
  pending_version?: string
  checked_at: string
  releases: AvailableRelease[]
}

export async function checkUpdates(force = false): Promise<ReleaseUpdateInfo> {
  const { data } = await apiClient.get<ReleaseUpdateInfo>('/admin/system/check-updates', { params: { force } })
  return data
}

export function createSystemOperationKey(operation: string): string {
  return `system-${operation}-${globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`}`
}

export async function performUpdate(version: string, idempotencyKey = createSystemOperationKey('update')): Promise<{ version: string; need_restart: boolean }> {
  const { data } = await apiClient.post<{ version: string; need_restart: boolean }>('/admin/system/update', { version }, { timeout: 15 * 60 * 1000, headers: { 'Idempotency-Key': idempotencyKey } })
  return data
}

export const systemAPI = { getVersion, restartService, checkUpdates, performUpdate }
export default systemAPI
