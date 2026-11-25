export type APIKeyWriteMode = 'append' | 'replace'

/** Accept one key per line; preserve order while removing empty and duplicate entries. */
export function parseAPIKeyInput(value: string): string[] {
  return [...new Set(value.split(/\r?\n/).map(key => key.trim()).filter(Boolean))]
}

export function applyAPIKeyInput(
  credentials: Record<string, unknown>,
  value: string,
  mode?: APIKeyWriteMode
): void {
  const keys = parseAPIKeyInput(value)
  if (keys.length === 0) return

  // Keep the legacy first key for older integrations and model-sync previews.
  credentials.api_key = keys[0]
  if (mode || keys.length > 1) credentials.api_keys = keys
  if (mode) credentials.api_keys_mode = mode
}

export function configuredAPIKeyCount(account: {
  credentials_status?: { api_key_count?: number; has_api_key?: boolean }
  credentials?: Record<string, unknown>
} | null): number {
  const status = account?.credentials_status
  if (typeof status?.api_key_count === 'number') return status.api_key_count
  if (status?.has_api_key) return 1
  const keys = account?.credentials?.api_keys
  if (Array.isArray(keys)) {
    return new Set(keys.filter((key): key is string => typeof key === 'string' && !!key.trim()).map(key => key.trim())).size
  }
  return account?.credentials?.api_key ? 1 : 0
}
