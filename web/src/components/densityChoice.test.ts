import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, test } from 'vitest'
import { useSettings } from '../stores/settings'
import DensityChoice from './DensityChoice.vue'

let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  localStorage.clear()
  pinia = createPinia()
  setActivePinia(pinia)
})

const open = () => mount(DensityChoice, { global: { plugins: [pinia] } })
const fresh = () => {
  setActivePinia(createPinia())
  return useSettings()
}

test('Comfortable is the default', () => {
  const w = open()
  expect(w.find('[aria-checked="true"]').text()).toBe('Comfortable')
  expect(w.text()).toContain('Taller rows')
})

test('the choice is remembered', async () => {
  const w = open()
  await w.findAll('[role="radio"]').find((b) => b.text() === 'Compact')!.trigger('click')
  await flushPromises()
  expect(w.text()).toContain('Shorter rows')
  expect(localStorage.getItem('shelf.density')).toBe('compact')
  expect(fresh().density).toBe('compact')
})

test('an unknown stored value falls back to the default', () => {
  localStorage.setItem('shelf.density', 'tiny')
  expect(fresh().density).toBe('comfortable')
})

test('the preview offers nothing to press', () => {
  const preview = open().find('.preview')
  expect(preview.attributes('inert')).toBeDefined()
  expect(preview.find('button').exists()).toBe(false)
  expect(preview.text()).toContain('Retry')
})
