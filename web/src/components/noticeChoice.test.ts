import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, test } from 'vitest'
import { useSettings } from '../stores/settings'
import NoticeChoice from './NoticeChoice.vue'

let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  localStorage.clear()
  pinia = createPinia()
  setActivePinia(pinia)
})

const open = () => mount(NoticeChoice, { global: { plugins: [pinia] } })
const fresh = () => {
  setActivePinia(createPinia())
  return useSettings()
}

test('Failures is the default', () => {
  const w = open()
  expect(w.find('[aria-checked="true"]').text()).toBe('Failures')
  expect(w.text()).toContain('fails or ends with errors')
})

test('the choice is remembered', async () => {
  const w = open()
  await w.findAll('[role="radio"]').find((b) => b.text() === 'All')!.trigger('click')
  await flushPromises()
  expect(localStorage.getItem('shelf.notices')).toBe('all')
  expect(fresh().notices).toBe('all')
})

test('an unknown stored value falls back to the default', () => {
  localStorage.setItem('shelf.notices', 'loud')
  expect(fresh().notices).toBe('failures')
})
