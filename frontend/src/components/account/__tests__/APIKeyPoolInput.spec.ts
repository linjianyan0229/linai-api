import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import APIKeyPoolInput from '../APIKeyPoolInput.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: { count: number }) => params ? `${key}:${params.count}` : key })
}))

describe('APIKeyPoolInput', () => {
  it('keeps pasted keys masked and switches to multiline input without losing them', async () => {
    const wrapper = mount(APIKeyPoolInput, { props: { modelValue: '' } })
    expect(wrapper.get('input').attributes('type')).toBe('password')
    await wrapper.setProps({ modelValue: 'one\ntwo' })
    expect(wrapper.get('textarea').element.value).toBe('one\ntwo')
    expect(wrapper.get('textarea').classes()).toContain('key-pool-masked')
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('textarea').classes()).not.toContain('key-pool-masked')
  })

  it('shows stored count, defaults to append, and warns when replacing', async () => {
    const Host = defineComponent({
      components: { APIKeyPoolInput },
      data: () => ({ keys: '', mode: 'append' as 'append' | 'replace' }),
      template: '<APIKeyPoolInput v-model="keys" v-model:mode="mode" editing :stored-count="3" />'
    })
    const wrapper = mount(Host)
    expect(wrapper.get('[data-testid="api-key-pool-stored-count"]').text()).toContain(':3')
    expect(wrapper.find('select').exists()).toBe(false)
    await wrapper.get('input').setValue('new-key')
    expect(wrapper.get('select').element.value).toBe('append')
    await wrapper.get('select').setValue('replace')
    expect(wrapper.text()).toContain('admin.accounts.apiKeyPool.replaceHint')
  })
})
