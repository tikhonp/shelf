import { mount } from '@vue/test-utils'
import { expect, test } from 'vitest'
import type { Format } from '../api/types'
import FormatMenu from './FormatMenu.vue'

const formats: Format[] = [
  { id: 'q:7', label: '24/96', description: 'FLAC up to 24-bit 96 kHz' },
  { id: 'q:27', label: 'Max', description: 'The best', default: true },
  { id: 'q:5', label: 'MP3 320', description: 'MP3 at 320 kbps' },
]

function menu(chosen?: string) {
  return mount(FormatMenu, { props: { formats, source: 'Qobuz', chosen }, attachTo: document.body })
}

test('the menu lists the source formats with the default first', () => {
  const w = menu()
  expect(w.find('.pop-h').text()).toBe('Qobuz format')
  const items = w.findAll('[role="menuitemradio"]')
  expect(items.map((b) => b.find('.bold').text())).toEqual(['Max', '24/96', 'MP3 320'])
  expect(items[0]?.find('.hint').text()).toBe('Default')
  expect(items[1]?.find('.hint').text()).toBe('')
  w.unmount()
})

test('the default is checked until another is chosen', () => {
  const first = menu()
  expect(first.findAll('[aria-checked="true"]').map((b) => b.find('.bold').text())).toEqual(['Max'])
  first.unmount()
  const w = menu('q:7')
  expect(w.findAll('[aria-checked="true"]').map((b) => b.find('.bold').text())).toEqual(['24/96'])
  w.unmount()
})

test('picking a format reports it', async () => {
  const w = menu()
  await w.findAll('[role="menuitemradio"]')[2]!.trigger('click')
  expect(w.emitted('pick')?.[0]).toEqual([formats[2]])
  w.unmount()
})

test('Escape closes it', async () => {
  const w = menu()
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
  expect(w.emitted('close')).toHaveLength(1)
  w.unmount()
})
