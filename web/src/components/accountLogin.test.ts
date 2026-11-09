import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import { createAppRouter } from '../router'
import { useAuth } from '../stores/auth'
import { useEditor } from '../stores/editor'
import { useTheme } from '../stores/theme'
import { FakeEventSource, Reply, list, stubApi } from '../testing/api'

beforeEach(() => {
  vi.stubGlobal('EventSource', FakeEventSource)
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function (this: HTMLDialogElement) { this.open = true } })
})
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

async function mountApp(day = false, path = '/search') {
  const pinia = createPinia(); setActivePinia(pinia)
  useTheme().day = day
  const router = createAppRouter(createMemoryHistory()); await router.push(path)
  const w = mount(App, { global: { plugins: [pinia, router] }, attachTo: document.body }); await flushPromises(); return w
}
const signedOut = { provider: 'simple', account: null, setup_needed: false }
const alice = { provider: 'simple', account: { id: 1, username: 'alice' }, setup_needed: false }
const appRoutes = { '/api/downloads': list([]), '/api/sources': [], '/api/library/scan': { enabled: false }, '/api/telegram': { enabled: false }, '/api/search': { items: [], total: 0 } }
const lcd = (w: Awaited<ReturnType<typeof mountApp>>) => w.find('[role="status"]').findAll('.lcd-l').map((l) => l.text())

// passkeyBrowser makes the browser offer passkeys, and get answers as given.
function passkeyBrowser(get: () => Promise<unknown>) {
  vi.stubGlobal('isSecureContext', true)
  vi.stubGlobal('PublicKeyCredential', class {})
  vi.stubGlobal('AuthenticatorAttestationResponse', class {})
  Object.defineProperty(navigator, 'credentials', { configurable: true, value: { get } })
}
function reduceMotion(on: boolean) {
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: on && q.includes('reduced-motion'), addEventListener() {}, removeEventListener() {} }))
}

async function tryPassword(w: Awaited<ReturnType<typeof mountApp>>, user = 'alice') {
  await w.find('input[name="username"]').setValue(user)
  await w.find('input[name="password"]').setValue('wrong-password')
  await w.find('form').trigger('submit')
  await flushPromises()
}

test('the console is switched off: tabs disabled, dark lamps, no clock', async () => {
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp()
  const tabs = w.findAll('.acct-tabs .mb-item')
  expect(tabs.map((t) => t.text())).toEqual(['Library', 'Search', 'Downloads', 'Status'])
  expect(tabs.every((t) => t.attributes('aria-disabled') === 'true')).toBe(true)
  expect(w.findAll('.acct-bar .leds .led.off')).toHaveLength(3)
  expect(w.find('.acct-clock').text()).toBe('--:--')
  expect(w.findAll('.acct-blank')).toHaveLength(2)
  expect(lcd(w)).toEqual(['SIGNED OUT', 'PASSWORD'])
  w.unmount()
})

test('the switched-off menubar shows the Inbox tab when the Inbox was on last time', async () => {
  localStorage.setItem('shelf.inbox', '1')
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp()
  expect(w.findAll('.acct-tabs .mb-item').map((t) => t.text())).toContain('Inbox')
  localStorage.removeItem('shelf.inbox')
  w.unmount()
})

test('a 401 shows Shelf words on the display, empties the password and focuses it', async () => {
  stubApi({ '/api/auth/login': new Reply(401, { error: 'Incorrect credentials or credentials changed; sign in again' }), '/api/auth': signedOut })
  const w = await mountApp()
  await tryPassword(w)
  expect(lcd(w)).toEqual(['WRONG USERNAME', 'OR PASSWORD'])
  expect(w.find('#acct-h .led').classes()).toContain('bad')
  expect(w.text()).not.toContain('Incorrect credentials')
  expect((w.find('input[name="password"]').element as HTMLInputElement).value).toBe('')
  expect(document.activeElement).toBe(w.find('input[name="password"]').element)
  w.unmount()
})

test('a 429 says to try again later, with no wait time', async () => {
  stubApi({ '/api/auth/login': new Reply(429, { error: 'too many attempts' }), '/api/auth': signedOut })
  const w = await mountApp()
  await tryPassword(w)
  expect(lcd(w)).toEqual(['TOO MANY TRIES', 'TRY AGAIN LATER'])
  w.unmount()
})

test('while the password is checked, the display names the user and Sign in is pressed', async () => {
  let release!: () => void
  stubApi({ '/api/auth/login': () => new Promise((resolve) => { release = () => resolve(new Reply(401, {})) }), '/api/auth': signedOut })
  const w = await mountApp()
  await tryPassword(w)
  expect(lcd(w)).toEqual(['CHECKING', 'ALICE'])
  const key = w.find('button[type="submit"]')
  expect(key.text()).toBe('Sign in')
  expect(key.classes()).toContain('pressed')
  expect(key.attributes('disabled')).toBeDefined()
  expect(w.find('#acct-h .led').classes()).toEqual(expect.arrayContaining(['warn', 'blink']))
  release(); await flushPromises()
  w.unmount()
})

test('the sign-in unit follows the Day theme', async () => {
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp(true)
  expect(w.find('.acct-desk').classes()).toContain('day')
  expect(w.find('.acct-desk').text()).toContain('Music Shelf')
  w.unmount()
})

test('while access is checked, the display says so, and a failed check offers Try again', async () => {
  let fail = true
  stubApi({ '/api/auth': () => (fail ? new Reply(502, { error: 'Bad gateway' }) : { provider: 'none', account: null, setup_needed: false }) })
  const w = await mountApp()
  expect(lcd(w)).toEqual(['NO ANSWER', 'TRY AGAIN'])
  expect(w.find('form').exists()).toBe(false)
  fail = false
  await w.findAll('button').find((b) => b.text() === 'Try again')!.trigger('click')
  await flushPromises()
  expect(w.find('.acct-desk').exists()).toBe(false)
  w.unmount()
})

test('with no accounts, the unit shows the command and a Reload key and no fields', async () => {
  stubApi({ '/api/auth': { provider: 'simple', account: null, setup_needed: true } })
  const w = await mountApp()
  expect(lcd(w)).toEqual(['NO ACCOUNTS', 'ADD ONE ON THE SERVER'])
  expect(w.find('pre').text()).toContain('shelf manage users add <username>')
  expect(w.find('input').exists()).toBe(false)
  expect(w.findAll('button').map((b) => b.text())).toContain('Reload')
  w.unmount()
})

test('after sign-in the lamps light 260 ms apart, the tab lights, then the app mounts', async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'setInterval'] })
  stubApi({ '/api/auth/login': alice, '/api/auth': signedOut, ...appRoutes })
  const w = await mountApp(false, '/search')
  await tryPassword(w)
  expect(lcd(w)).toEqual(['HELLO, ALICE', 'OPENING SEARCH'])
  expect(w.find('.acct-tabs .mb-item.on').text()).toBe('Search')
  expect(w.find('.acct-clock').text()).not.toBe('--:--')
  expect(w.findAll('.acct-bar .leds .led.ok')).toHaveLength(0)
  await vi.advanceTimersByTimeAsync(260)
  expect(w.findAll('.acct-bar .leds .led.ok')).toHaveLength(1)
  await vi.advanceTimersByTimeAsync(520)
  expect(w.findAll('.acct-bar .leds .led.ok')).toHaveLength(3)
  expect(useAuth().mounted).toBe(false)
  await vi.advanceTimersByTimeAsync(260); await flushPromises()
  expect(useAuth().mounted).toBe(true)
  expect(w.find('.acct-desk').exists()).toBe(false)
  w.unmount()
})

test('with reduced motion, the app mounts at once', async () => {
  reduceMotion(true)
  stubApi({ '/api/auth/login': alice, '/api/auth': signedOut, ...appRoutes })
  const w = await mountApp()
  await tryPassword(w)
  expect(useAuth().mounted).toBe(true)
  w.unmount()
})

test('a canceled passkey turns the key back to OFF and asks for the password', async () => {
  passkeyBrowser(() => Promise.reject(new DOMException('canceled', 'NotAllowedError')))
  stubApi({ '/api/auth/passkey/begin': { publicKey: { challenge: 'AAAA' } }, '/api/auth': signedOut })
  const w = await mountApp()
  expect(lcd(w)).toEqual(['SIGNED OUT', 'PASSWORD OR PASSKEY'])
  await w.findAll('button').find((b) => b.text() === 'Sign in with a passkey')!.trigger('click')
  await flushPromises()
  expect(lcd(w)).toEqual(['PASSKEY CANCELED', 'USE THE PASSWORD'])
  expect(w.find('.keysw-bow').classes()).not.toContain('on')
  w.unmount()
})

test('a passkey sign-in turns the key to ON', async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'setInterval'] })
  passkeyBrowser(() => Promise.resolve(Object.assign(Object.create(null), { id: 'k', rawId: new ArrayBuffer(1), type: 'public-key', response: { clientDataJSON: new ArrayBuffer(1), authenticatorData: new ArrayBuffer(1), signature: new ArrayBuffer(1), userHandle: null }, getClientExtensionResults: () => ({}) })))
  stubApi({ '/api/auth/passkey/begin': { publicKey: { challenge: 'AAAA' } }, '/api/auth/passkey/finish': alice, '/api/auth': signedOut, ...appRoutes })
  const w = await mountApp()
  await w.findAll('button').find((b) => b.text() === 'Sign in with a passkey')!.trigger('click')
  await flushPromises()
  expect(lcd(w)).toEqual(['HELLO, ALICE', 'OPENING SEARCH'])
  expect(w.find('.keysw-bow').classes()).toContain('on')
  w.unmount()
})

test('a session that ends inside the app shows the edits it kept and the same username', async () => {
  let ended = false
  stubApi({ '/api/auth': () => (ended ? signedOut : alice), '/api/auth/login': new Reply(401, {}), ...appRoutes })
  const w = await mountApp()
  ended = true
  const editor = useEditor()
  // Genre on two tracks of one album is one edit; a cover on another album is the second.
  editor.held = { 4: { stage: { 11: { 'field:genre': { values: ['Jazz'] } }, 12: { 'field:genre': { values: ['Jazz'] } } } as never, cover: null }, 9: { stage: {}, cover: { upload: 'c' } } }
  useAuth().expired(); await flushPromises()
  const dialog = w.find('dialog')
  expect(dialog.find('h1').text()).toBe('Sign in again')
  expect(dialog.find('[role="status"]').text()).toContain('SESSION ENDED')
  expect(dialog.find('[role="status"]').text()).toContain('2 EDITS KEPT')
  const user = dialog.find('input[name="username"]')
  expect((user.element as HTMLInputElement).value).toBe('alice')
  expect(user.attributes('readonly')).toBeDefined()
  expect(dialog.text()).toContain('Sign in as alice to go on where you stopped.')
  const keys = dialog.findAll('.account-keys button').map((b) => b.text())
  expect(keys.at(-1)).toBe('Sign in')
  await dialog.findAll('button').find((b) => b.text() === 'Not alice?')!.trigger('click')
  expect((user.element as HTMLInputElement).value).toBe('')
  expect(user.attributes('readonly')).toBeUndefined()
  await user.setValue('bob')
  await dialog.find('input[name="password"]').setValue('wrong')
  await dialog.find('form').trigger('submit'); await flushPromises()
  expect(dialog.find('[role="status"]').text()).toContain('WRONG USERNAME')
  w.unmount()
})
