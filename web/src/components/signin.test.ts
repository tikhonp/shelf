import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { Wrapper } from '../api/types'
import { useStatus } from '../stores/status'
import { Reply, stubApi } from '../testing/api'
import SignIn from './SignIn.vue'

const wrapper = (state: Wrapper['state'], over: Partial<Wrapper> = {}): Wrapper => ({ state, storefront: '', since: '', error: '', code_deadline: null, ...over })

let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

function open() {
  return mount(SignIn, { global: { plugins: [pinia] }, attachTo: document.body })
}

async function credentials(w: ReturnType<typeof open>) {
  await w.find('input[type="email"]').setValue('me@example.com')
  await w.find('input[type="password"]').setValue('secret')
  await w.find('form').trigger('submit')
  await flushPromises()
}

test('the dialog moves from credentials to code to done', async () => {
  const calls = stubApi({
    '/api/applemusic/wrapper/login': wrapper('signing_in'),
    '/api/applemusic/wrapper/code': wrapper('signing_in'),
    '/api/applemusic/wrapper': wrapper('signing_in'),
  })
  const w = open()
  expect(w.text()).toContain('Sign the wrapper in to an Apple ID')
  await credentials(w)
  expect(JSON.parse(calls[0]!.init!.body as string)).toEqual({ username: 'me@example.com', password: 'secret' })
  expect(w.text()).toContain('SIGNING IN')

  const status = useStatus()
  status.applyWrapper(wrapper('awaiting_code', { code_deadline: new Date(Date.now() + 41_500).toISOString() }))
  await flushPromises()
  expect(w.text()).toContain('Enter the code sent to your Apple devices')
  expect(w.find('.vfd').text()).toBe('0:42')
  const confirm = () => w.findAll('button').find((b) => b.text() === 'Confirm')!
  expect(confirm().attributes('disabled')).toBeDefined()

  const first = w.findAll('.digit')[0]!
  await first.trigger('paste', { clipboardData: { getData: () => '481937' } })
  await flushPromises()
  expect(confirm().attributes('disabled')).toBeUndefined()
  await w.findAll('form')[0]!.trigger('submit')
  await flushPromises()
  expect(JSON.parse(calls.at(-1)!.init!.body as string)).toEqual({ code: '481937' })

  status.applyWrapper(wrapper('running', { storefront: 'ru' }))
  await flushPromises()
  expect(w.text()).toContain('Signed in, storefront RU')
  w.unmount()
})

test('the countdown running out shows the timeout', async () => {
  vi.useFakeTimers()
  const waiting = wrapper('awaiting_code', { code_deadline: new Date(Date.now() + 2000).toISOString() })
  stubApi({ '/api/applemusic/wrapper': waiting })
  useStatus().wrapper = waiting
  const w = open()
  expect(w.text()).toContain('Enter the code')
  await vi.advanceTimersByTimeAsync(3000)
  expect(w.text()).toContain("The code wasn't entered in time. Try again.")
  w.unmount()
})

test("the wrapper's own timeout shows the same", async () => {
  stubApi({ '/api/applemusic/wrapper': wrapper('awaiting_code') })
  const status = useStatus()
  status.wrapper = wrapper('awaiting_code')
  const w = open()
  status.applyWrapper(wrapper('error', { error: '2FA code timeout (60s), aborting login' }))
  await flushPromises()
  expect(w.text()).toContain("The code wasn't entered in time. Try again.")
  w.unmount()
})

test('a failed sign-in keeps its message after the wrapper clears it', async () => {
  stubApi({ '/api/applemusic/wrapper/login': wrapper('signing_in'), '/api/applemusic/wrapper': wrapper('signing_in') })
  const w = open()
  await credentials(w)
  const status = useStatus()
  status.applyWrapper(wrapper('error', { error: 'server message: Your Apple ID or password was incorrect.' }))
  await flushPromises()
  expect(w.text()).toContain('Your Apple ID or password was incorrect.')
  status.applyWrapper(wrapper('running', { storefront: 'ru' }))
  await flushPromises()
  expect(w.text()).toContain('Your Apple ID or password was incorrect.')
  expect(w.text()).not.toContain('Signed in')
  w.unmount()
})

test('Try again returns to the credentials without the password', async () => {
  stubApi({ '/api/applemusic/wrapper/login': wrapper('signing_in'), '/api/applemusic/wrapper': wrapper('signing_in') })
  const w = open()
  await credentials(w)
  useStatus().applyWrapper(wrapper('error', { error: 'auth error' }))
  await flushPromises()
  await w.findAll('button').find((b) => b.text() === 'Try again')!.trigger('click')
  await flushPromises()
  expect((w.find('input[type="email"]').element as HTMLInputElement).value).toBe('me@example.com')
  expect((w.find('input[type="password"]').element as HTMLInputElement).value).toBe('')
  w.unmount()
})

test('a refused sign-in stays on the credentials and says why', async () => {
  stubApi({ '/api/applemusic/wrapper/login': new Reply(409, { error: 'a sign-in is running' }) })
  const w = open()
  await credentials(w)
  expect(w.find('input[type="email"]').exists()).toBe(true)
  expect(w.text()).toContain('a sign-in is running')
  w.unmount()
})

test('a wrong code stays on the code step', async () => {
  stubApi({ '/api/applemusic/wrapper/code': new Reply(400, { error: 'the code must be six digits' }), '/api/applemusic/wrapper': wrapper('awaiting_code') })
  useStatus().wrapper = wrapper('awaiting_code')
  const w = open()
  await w.findAll('.digit')[0]!.trigger('paste', { clipboardData: { getData: () => '123456' } })
  await w.find('form').trigger('submit')
  await flushPromises()
  expect(w.text()).toContain('the code must be six digits')
  expect(w.text()).toContain('Enter the code')
  w.unmount()
})

test('closing during a sign-in stops it', async () => {
  const calls = stubApi({ '/api/applemusic/wrapper/restart': wrapper('starting'), '/api/applemusic/wrapper': wrapper('awaiting_code') })
  useStatus().wrapper = wrapper('awaiting_code')
  const w = open()
  await w.find('button[aria-label="Close"]').trigger('click')
  await flushPromises()
  expect(calls.map((c) => c.url)).toContain('/api/applemusic/wrapper/restart')
  expect(w.emitted('close')).toHaveLength(1)
  w.unmount()
})
