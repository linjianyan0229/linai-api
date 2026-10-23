import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { CandyMonitorView } from '@/api/channelMonitor'
import zh from '@/i18n/locales/zh/dashboard'
import MonitorIntelligenceBar from '../MonitorIntelligenceBar.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: { value: 'zh-CN' },
    t: (key: string, params?: Record<string, unknown>) => {
      let value: unknown = zh
      for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
      return String(value ?? key).replace('{rate}', String(params?.rate ?? ''))
    }
  })
}))

function view(overrides: Partial<CandyMonitorView> = {}): CandyMonitorView {
  return { enabled: true, model: 'configured-model', status: 'pending', running: false, interval_seconds: 600, history: [], ...overrides }
}
function render(value: CandyMonitorView) {
  return mount(MonitorIntelligenceBar, { props: { value } })
}

describe('monitor intelligence history', () => {
  it('keeps a new monitor pending and does not show a healthy result before any check', () => {
    const wrapper = render(view())
    expect(wrapper.text()).toContain('待检测')
    expect(wrapper.text()).toContain('暂无有效结果')
    expect(wrapper.text()).not.toContain('正常')
    expect(wrapper.findAll('[data-status="empty"]')).toHaveLength(60)
    expect(wrapper.get('[role="img"]').attributes('aria-label')).toContain('configured-model')
  })

  it('distinguishes an incorrect answer from a failed test and shows the actual history', async () => {
    const history = [
      { status: 'passed' as const, checked_at: '2026-10-23T01:00:00Z' },
      { status: 'incorrect' as const, checked_at: '2026-10-23T01:10:00Z' },
      { status: 'error' as const, checked_at: '2026-10-23T01:20:00Z' }
    ]
    const wrapper = render(view({ status: 'error', pass_rate: 50, history }))
    expect(wrapper.text()).toContain('检测失败')
    expect(wrapper.text()).toContain('通过率 50%')
    expect(wrapper.findAll('[data-status="passed"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-status="incorrect"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-status="error"]')).toHaveLength(1)
    expect(wrapper.get('[data-status="incorrect"]').attributes('title')).toContain('疑似降智')
    await wrapper.setProps({ value: view({ status: 'incorrect', history }) })
    expect(wrapper.text()).toContain('疑似降智')
    await wrapper.setProps({ value: view({ status: 'incorrect', running: true, history }) })
    expect(wrapper.text()).toContain('检测中')
  })
})
