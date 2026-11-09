import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import { createAppRouter } from '../router'
import { useTheme } from '../stores/theme'
import { FakeEventSource, Reply, stubApi } from '../testing/api'

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource)
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function (this: HTMLDialogElement) { this.open = true } })
})
async function mountApp(day = false) {
  const pinia = createPinia(); setActivePinia(pinia)
  useTheme().day = day
  const router = createAppRouter(createMemoryHistory()); await router.push('/search')
  const w = mount(App, { global: { plugins: [pinia, router] }, attachTo: document.body }); await flushPromises(); return w
}
const signedOut = { provider: 'simple', account: null, setup_needed: false }

async function tryPassword(w: Awaited<ReturnType<typeof mountApp>>) {
  await w.find('input[name="username"]').setValue('alice')
  await w.find('input[name="password"]').setValue('wrong-password')
  await w.find('form').trigger('submit')
  await flushPromises()
}

test('a 401 shows Shelf words in the error color, empties the password and focuses it', async () => {
  stubApi({ '/api/auth/login': new Reply(401, { error: 'Incorrect credentials or credentials changed; sign in again' }), '/api/auth': signedOut })
  const w = await mountApp()
  await tryPassword(w)
  const alert = w.find('[role="alert"]')
  expect(alert.text()).toBe('Wrong username or password.')
  expect(alert.classes()).toContain('bad-text')
  expect(w.text()).not.toContain('Incorrect credentials')
  expect((w.find('input[name="password"]').element as HTMLInputElement).value).toBe('')
  expect(document.activeElement).toBe(w.find('input[name="password"]').element)
  w.unmount()
})

test('a 429 says to try again later', async () => {
  stubApi({ '/api/auth/login': new Reply(429, { error: 'too many attempts' }), '/api/auth': signedOut })
  const w = await mountApp()
  await tryPassword(w)
  expect(w.find('[role="alert"]').text()).toBe('Too many tries. Try again later.')
  w.unmount()
})

test('the main key says Sign in, and the fields are plain inputs', async () => {
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp()
  expect(w.find('h1').text()).toBe('Sign in to Shelf')
  expect(w.find('button[type="submit"]').text()).toBe('Sign in')
  expect(w.find('input[name="username"]').classes()).toContain('input')
  expect(w.find('input[name="password"]').classes()).toContain('input')
  w.unmount()
})

test('the sign-in window and the console behind it follow the Day theme', async () => {
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp(true)
  expect(w.find('dialog').classes()).toContain('day')
  expect(w.find('.acct-desk').classes()).toContain('day')
  expect(w.find('.acct-desk').text()).toContain('Music Shelf')
  w.unmount()
})

test('while access is checked, a display says so, and a failed check offers Try again', async () => {
  let fail = true
  stubApi({ '/api/auth': () => (fail ? new Reply(502, { error: 'Bad gateway' }) : { provider: 'none', account: null, setup_needed: false }) })
  const w = await mountApp()
  expect(w.find('.acct-check [role="status"]').text()).toContain('NO ANSWER')
  fail = false
  await w.find('.acct-check button').trigger('click')
  await flushPromises()
  expect(w.find('.acct-desk').exists()).toBe(false)
  w.unmount()
})
