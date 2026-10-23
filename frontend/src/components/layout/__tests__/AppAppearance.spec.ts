import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick, Teleport } from 'vue'
import AppAppearance from '../AppAppearance.vue'

let wrapper: VueWrapper | undefined

function pointer(target: EventTarget, type: string) {
  const event = new MouseEvent(type, { bubbles: true, button: 0 })
  Object.defineProperty(event, 'pointerType', { value: 'mouse' })
  target.dispatchEvent(event)
}

function mountAppearance() {
  wrapper = mount(AppAppearance, {
    attachTo: document.body,
    slots: {
      default: defineComponent({
        render: () => h('main', [
          h('button', { id: 'page-control' }, 'Page action'),
          h('button', { id: 'disabled-control', disabled: true }, 'Disabled'),
          h(Teleport, { to: 'body' }, [
            h('div', { class: 'modal-content' }, [h('button', { id: 'dialog-control' }, 'Save')]),
            h('div', { class: 'liquid-popover' }, [h('button', { id: 'menu-row' }, 'Option')])
          ])
        ])
      })
    }
  })
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('global Liquid Glass interaction lifecycle', () => {
  it('handles page and teleported dialog controls through one global owner', () => {
    mountAppearance()
    expect(document.body.classList.contains('liquid-app')).toBe(true)
    expect(document.body.style.getPropertyValue('--liquid-refraction')).toContain('url(#liquid-lens-')
    const page = document.getElementById('page-control')!
    const dialog = document.getElementById('dialog-control')!
    pointer(page, 'pointerdown')
    expect(page.classList.contains('liquid-pressed')).toBe(true)
    pointer(dialog, 'pointerdown')
    expect(page.classList.contains('liquid-pressed')).toBe(false)
    expect(dialog.classList.contains('liquid-pressed')).toBe(true)
    pointer(window, 'pointerup')
    expect(dialog.classList.contains('liquid-pressed')).toBe(false)
  })

  it('leaves disabled controls and menu rows with their own selection feedback', () => {
    mountAppearance()
    pointer(document.getElementById('disabled-control')!, 'pointerdown')
    pointer(document.getElementById('menu-row')!, 'pointerdown')
    expect(document.querySelector('.liquid-pressed')).toBeNull()
  })

  it('clears press feedback on nested scrolling and removes listeners on unmount', () => {
    mountAppearance()
    const control = document.getElementById('page-control')!
    pointer(control, 'pointerdown')
    control.parentElement!.dispatchEvent(new Event('scroll'))
    expect(control.classList.contains('liquid-pressed')).toBe(false)
    wrapper!.unmount()
    wrapper = undefined
    document.body.appendChild(control)
    pointer(control, 'pointerdown')
    expect(control.classList.contains('liquid-pressed')).toBe(false)
    expect(document.body.classList.contains('liquid-app')).toBe(false)
    expect(document.body.style.getPropertyValue('--liquid-refraction')).toBe('')
  })

  it('respects reduced motion and clears feedback when the window loses focus', async () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList)
    mountAppearance()
    const control = document.getElementById('page-control')!
    pointer(control, 'pointermove')
    expect(control.classList.contains('liquid-interacting')).toBe(false)
    pointer(control, 'pointerdown')
    window.dispatchEvent(new Event('blur'))
    await nextTick()
    expect(control.classList.contains('liquid-pressed')).toBe(false)
  })
})
