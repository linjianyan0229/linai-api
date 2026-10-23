import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import Toggle from '../Toggle.vue'

let wrapper: VueWrapper | undefined
afterEach(() => wrapper?.unmount())

function pointer(type: string, x: number, y = 10) {
  const event = new MouseEvent(type, { bubbles: true, button: 0, clientX: x, clientY: y })
  Object.defineProperties(event, { pointerId: { value: 1 }, isPrimary: { value: true }, pointerType: { value: 'touch' } })
  wrapper!.element.dispatchEvent(event)
}

describe('Liquid Glass switch interaction', () => {
  it('preserves button activation and accessible checked state', async () => {
    wrapper = mount(Toggle, { props: { modelValue: false } })
    await wrapper.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    await wrapper.setProps({ modelValue: true })
    expect(wrapper.attributes('aria-checked')).toBe('true')
    expect(wrapper.attributes('style')).toContain('--switch-progress: 1')
  })

  it('follows a drag, commits once on release, and suppresses the trailing click', async () => {
    wrapper = mount(Toggle, { props: { modelValue: false } })
    pointer('pointerdown', 0)
    pointer('pointermove', 18)
    await wrapper.vm.$nextTick()
    expect(wrapper.attributes('data-dragging')).toBe('true')
    expect(wrapper.attributes('style')).toContain('--switch-progress: 0.9')
    pointer('pointerup', 18)
    wrapper.element.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 1 }))
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    expect(wrapper.attributes('data-pressing')).toBeUndefined()
  })

  it('cancels vertical scrolling and pointer cancellation without toggling', async () => {
    wrapper = mount(Toggle, { props: { modelValue: false } })
    pointer('pointerdown', 0)
    pointer('pointermove', 2, 35)
    pointer('pointerup', 2, 35)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    pointer('pointerdown', 0)
    pointer('pointermove', 18)
    pointer('pointercancel', 18)
    await wrapper.vm.$nextTick()
    expect(wrapper.attributes('data-pressing')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('accepts a new click immediately after a completed drag', async () => {
    wrapper = mount(Toggle, { props: { modelValue: false } })
    pointer('pointerdown', 0)
    pointer('pointermove', 18)
    pointer('pointerup', 18)
    await wrapper.setProps({ modelValue: true })
    pointer('pointerdown', 18)
    pointer('pointerup', 18)
    wrapper.element.dispatchEvent(new MouseEvent('click', { bubbles: true, detail: 1 }))
    expect(wrapper.emitted('update:modelValue')).toEqual([[true], [false]])
  })

  it('ignores disabled switches and cancels when disabled during a drag', async () => {
    wrapper = mount(Toggle, { props: { modelValue: false, disabled: true } })
    pointer('pointerdown', 0)
    pointer('pointermove', 18)
    pointer('pointerup', 18)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ disabled: false })
    pointer('pointerdown', 0)
    pointer('pointermove', 18)
    await wrapper.setProps({ disabled: true })
    pointer('pointerup', 18)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.attributes('data-pressing')).toBeUndefined()
  })
})
