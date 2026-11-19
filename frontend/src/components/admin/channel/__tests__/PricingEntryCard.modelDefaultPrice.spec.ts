import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PricingEntryCard from '../PricingEntryCard.vue'
import type { IntervalFormEntry, PricingFormEntry } from '../types'
import channelsAPI, { type ModelDefaultPricing } from '@/api/admin/channels'

vi.mock('@/api/admin/channels', () => ({
  default: { getModelDefaultPricing: vi.fn() },
}))

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const getModelDefaultPricing = vi.mocked(channelsAPI.getModelDefaultPricing)

function createEntry(models: string[] = []): PricingFormEntry {
  return {
    models,
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    ultrafast_multiplier: null,
    reasoning_effort_multipliers: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_pricing: {
      timezone: 'Asia/Shanghai',
      weekdays_only: false,
      periods: [],
    },
  }
}

function addModels(entry: PricingFormEntry, models: string[], platform?: string) {
  const wrapper = shallowMount(PricingEntryCard, {
    props: {
      entry,
      platform,
      onUpdate: (value: PricingFormEntry) => { void wrapper.setProps({ entry: value }) },
    },
  })
  wrapper.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', models)
  return wrapper
}

function deferredPricing() {
  let resolve!: (value: ModelDefaultPricing) => void
  const promise = new Promise<ModelDefaultPricing>(done => { resolve = done })
  return { promise, resolve }
}

async function editPrice(wrapper: ReturnType<typeof shallowMount>, labelKey: string, value: string) {
  const label = wrapper.findAll('label').find(label => label.text() === labelKey)
  expect(label).toBeDefined()
  const input = label!.element.parentElement!.querySelector('input')!
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}

const emittedEntries = (wrapper: ReturnType<typeof shallowMount>) =>
  (wrapper.emitted('update') ?? []).map(e => e[0] as PricingFormEntry)

describe('PricingEntryCard default price auto-fill', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getModelDefaultPricing.mockResolvedValue({ found: true, input_price: 3e-6, output_price: 15e-6 })
  })

  it('auto-fills the default price when a model is added to an empty entry', async () => {
    const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).toHaveBeenCalledTimes(1)
    expect(getModelDefaultPricing).toHaveBeenCalledWith('claude-opus-4-8')
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-8'],
      input_price: 3,
      output_price: 15,
    })
  })

  it('does not overwrite prices when appending a model to an entry that already has one', async () => {
    const entry = createEntry(['claude-opus-4-7'])
    const wrapper = addModels(entry, ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-7', 'claude-opus-4-8'],
    })
    for (const emitted of emittedEntries(wrapper)) {
      expect(emitted.input_price).toBeNull()
      expect(emitted.output_price).toBeNull()
      expect(emitted.cache_write_price).toBeNull()
      expect(emitted.cache_read_price).toBeNull()
    }
  })

  it('does not auto-fill a shared price when several models are added to an empty entry at once', async () => {
    const wrapper = addModels(createEntry(), ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-7', 'claude-opus-4-8'],
      input_price: null,
      output_price: null,
    })
  })

  it('keeps manual prices untouched when a model is appended', async () => {
    const entry = { ...createEntry(['claude-opus-4-7']), input_price: '1.5' }
    const wrapper = addModels(entry, ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    for (const emitted of emittedEntries(wrapper)) {
      expect(emitted.input_price).toBe('1.5')
    }
  })

  it.each([
    ['input_price', 'admin.channels.form.inputPrice', '9'],
    ['cache_write_12h_price', 'admin.channels.form.cacheWrite12hPrice', '0'],
  ] as const)('does not replace %s entered while the default request is pending', async (field, label, value) => {
    const pending = deferredPricing()
    getModelDefaultPricing.mockReturnValue(pending.promise)
    const wrapper = addModels(createEntry(), ['gpt-6.2-sol'], 'openai')
    await editPrice(wrapper, label, value)
    const updatesBeforeResponse = emittedEntries(wrapper).length
    pending.resolve({ found: true, input_price: 3e-6, output_price: 15e-6, cache_write_12h_price: 8e-6 })
    await flushPromises()

    expect(emittedEntries(wrapper)).toHaveLength(updatesBeforeResponse)
    expect(emittedEntries(wrapper).at(-1)?.[field]).toBe(value)
    wrapper.unmount()
  })

  it('does not restore a price the user entered and then cleared while waiting', async () => {
    const pending = deferredPricing()
    getModelDefaultPricing.mockReturnValue(pending.promise)
    const wrapper = addModels(createEntry(), ['gpt-6.2-sol'])
    await editPrice(wrapper, 'admin.channels.form.inputPrice', '9')
    await editPrice(wrapper, 'admin.channels.form.inputPrice', '')
    const updatesBeforeResponse = emittedEntries(wrapper).length
    pending.resolve({ found: true, input_price: 3e-6, output_price: 15e-6 })
    await flushPromises()

    expect(emittedEntries(wrapper)).toHaveLength(updatesBeforeResponse)
    expect(emittedEntries(wrapper).at(-1)?.input_price).toBeNull()
    wrapper.unmount()
  })

  it.each([{ models: [] }, { models: ['gpt-6.2-astra'] }])('ignores a delayed response after the model list changes to $models', async ({ models }) => {
    const pending = deferredPricing()
    getModelDefaultPricing.mockReturnValue(pending.promise)
    const wrapper = addModels(createEntry(), ['gpt-6.2-sol'])
    await flushPromises()
    wrapper.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', models)
    await flushPromises()
    const updatesBeforeResponse = emittedEntries(wrapper).length
    pending.resolve({ found: true, input_price: 3e-6, output_price: 15e-6 })
    await flushPromises()

    expect(emittedEntries(wrapper)).toHaveLength(updatesBeforeResponse)
    expect(emittedEntries(wrapper).at(-1)?.models).toEqual(models)
    wrapper.unmount()
  })

  it('keeps an explicit 12h zero price instead of starting an auto-fill request', async () => {
    const wrapper = addModels({ ...createEntry(), cache_write_12h_price: 0 }, ['gpt-6.2-sol'])
    await flushPromises()
    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)?.cache_write_12h_price).toBe(0)
    wrapper.unmount()
  })

  it.each([
    ['image_input_price', 0],
    ['image_input_price', 7],
    ['image_output_price', 0],
    ['image_output_price', 11],
  ] as const)('keeps the existing %s price %s without requesting defaults', async (field, value) => {
    const wrapper = addModels({ ...createEntry(), [field]: value }, ['gpt-image-2.6-flare'])
    await flushPromises()
    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)?.[field]).toBe(value)
    wrapper.unmount()
  })

  it('keeps time pricing and intervals edited while an otherwise untouched default is pending', async () => {
    const pending = deferredPricing()
    getModelDefaultPricing.mockReturnValue(pending.promise)
    const wrapper = addModels(createEntry(), ['gpt-6.2-sol'])
    await flushPromises()
    const entry = emittedEntries(wrapper).at(-1)!
    const timePricing = { timezone: 'UTC', weekdays_only: true, periods: [{ start_time: '08:00:00', end_time: '10:00:00', multiplier: '1.25' }] }
    const intervals: IntervalFormEntry[] = [{
      min_tokens: 0, max_tokens: 10000, tier_label: 'custom',
      input_price: '2', output_price: null, cache_write_price: null,
      cache_write_12h_price: '0', cache_read_price: null, per_request_price: null,
      input_multiplier: null, output_multiplier: null,
      cache_write_multiplier: null, cache_read_multiplier: null, sort_order: 0,
    }]
    await wrapper.setProps({ entry: { ...entry, time_pricing: timePricing, intervals } })
    pending.resolve({ found: true, input_price: 3e-6, output_price: 15e-6 })
    await flushPromises()
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({ input_price: 3, output_price: 15, time_pricing: timePricing, intervals })
    wrapper.unmount()
  })
})
