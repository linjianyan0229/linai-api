import { describe, expect, it } from 'vitest'
import { applyAPIKeyInput, configuredAPIKeyCount, parseAPIKeyInput } from '../apiKeyPool'

describe('API key pool input', () => {
  it('accepts Windows line endings and deduplicates while preserving rotation order', () => {
    expect(parseAPIKeyInput(' key-b \r\n\r\n key-a\nkey-b\n')).toEqual(['key-b', 'key-a'])
  })

  it('keeps single-key creation compatible and stores a pool for multiple keys', () => {
    const single: Record<string, unknown> = { base_url: 'https://example.test' }
    applyAPIKeyInput(single, ' first-key ')
    expect(single).toEqual({ base_url: 'https://example.test', api_key: 'first-key' })

    const multiple: Record<string, unknown> = {}
    applyAPIKeyInput(multiple, 'first-key\nsecond-key\nfirst-key')
    expect(multiple).toEqual({ api_key: 'first-key', api_keys: ['first-key', 'second-key'] })
  })

  it.each(['append', 'replace'] as const)('sends an explicit %s update, including a one-key pool', mode => {
    const credentials: Record<string, unknown> = {}
    applyAPIKeyInput(credentials, 'new-key', mode)
    expect(credentials).toEqual({ api_key: 'new-key', api_keys: ['new-key'], api_keys_mode: mode })
  })

  it('leaves stored credentials untouched for blank edit input', () => {
    const credentials = { base_url: 'https://example.test' }
    applyAPIKeyInput(credentials, ' \r\n ', 'replace')
    expect(credentials).toEqual({ base_url: 'https://example.test' })
  })

  it('reads the redacted count and falls back to legacy single-key metadata', () => {
    expect(configuredAPIKeyCount({ credentials_status: { api_key_count: 3, has_api_key: true } })).toBe(3)
    expect(configuredAPIKeyCount({ credentials_status: { has_api_key: true } })).toBe(1)
    expect(configuredAPIKeyCount({ credentials: { api_keys: ['a', 'b', 'a'] } })).toBe(2)
    expect(configuredAPIKeyCount({ credentials: { api_key: 'legacy' } })).toBe(1)
    expect(configuredAPIKeyCount(null)).toBe(0)
  })
})
