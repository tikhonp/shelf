import { expect, test } from 'vitest'
import { libraryAction } from './libraryKeys'

function press(key: string, tag = 'BODY', over: KeyboardEventInit = {}) {
  const e = new KeyboardEvent('keydown', { key, ...over })
  Object.defineProperty(e, 'target', { value: document.createElement(tag) })
  return libraryAction(e)
}

test('plain keys act outside a field', () => {
  expect(press('h')).toBe('history')
  expect(press('H')).toBe('history')
  expect(press('?')).toBe('keys')
  expect(press('2')).toBe('suggest-2')
  expect(press(' ')).toBe('play')
  expect(press('z')).toBeNull()
})

test('typing in a field stays typing, and Save, Discard and Esc still work', () => {
  expect(press('h', 'INPUT')).toBeNull()
  expect(press('1', 'TEXTAREA')).toBeNull()
  expect(press('s', 'INPUT', { ctrlKey: true })).toBe('save')
  expect(press('s', 'INPUT', { metaKey: true })).toBe('save')
  expect(press('d', 'INPUT', { ctrlKey: true, shiftKey: true })).toBe('discard')
  expect(press('Escape', 'INPUT')).toBe('leave')
})

test('a browser shortcut and a focused button keep their keys', () => {
  expect(press('r', 'BODY', { metaKey: true })).toBeNull()
  expect(press('Enter', 'BUTTON')).toBeNull()
  expect(press(' ', 'BUTTON')).toBeNull()
})
