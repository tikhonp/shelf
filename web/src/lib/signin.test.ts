import { expect, test } from 'vitest'
import type { Wrapper } from '../api/types'
import { fillDigits, nextStep, startStep } from './signin'

const w = (state: Wrapper['state'], error = ''): Wrapper => ({ state, storefront: '', since: '', error, code_deadline: null })

test('the steps follow the wrapper', () => {
  expect(nextStep('working', w('awaiting_code'))).toBe('code')
  expect(nextStep('code', w('signing_in'))).toBe('working')
  expect(nextStep('working', w('running'))).toBe('done')
  expect(nextStep('working', w('error', 'auth error'))).toBe('failed')
  expect(nextStep('code', w('error', '2FA code timeout (60s), aborting login'))).toBe('timeout')
})

test('a step that ended stays when the wrapper moves on', () => {
  // A wrapper with an old session clears its error within seconds.
  expect(nextStep('failed', w('running'))).toBe('failed')
  expect(nextStep('timeout', w('running'))).toBe('timeout')
  expect(nextStep('credentials', w('running'))).toBe('credentials')
})

test('the dialog joins a sign-in that already runs', () => {
  expect(startStep(w('awaiting_code'))).toBe('code')
  expect(startStep(w('signing_in'))).toBe('working')
  expect(startStep(w('signed_out'))).toBe('credentials')
  expect(startStep(null)).toBe('credentials')
})

test('pasting the whole code fills all six', () => {
  expect(fillDigits(['', '', '', '', '', ''], 0, '481937')).toEqual({ digits: ['4', '8', '1', '9', '3', '7'], focus: 5 })
  expect(fillDigits(['', '', '', '', '', ''], 0, '48 19-37')).toEqual({ digits: ['4', '8', '1', '9', '3', '7'], focus: 5 })
})

test('a digit moves on, and anything else clears the box', () => {
  expect(fillDigits(['4', '', '', '', '', ''], 1, '8')).toEqual({ digits: ['4', '8', '', '', '', ''], focus: 2 })
  expect(fillDigits(['4', '8', '', '', '', ''], 1, 'x').digits).toEqual(['4', '', '', '', '', ''])
})
