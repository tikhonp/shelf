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
// lcd reads the display under its MUSIC SHELF line, whether it is a status or an alert.
const lcd = (w: Awaited<ReturnType<typeof mountApp>>) => w.find('.lcd').findAll('.lcd-l:not(.dim)').map((l) => l.text())
const slide = (w: Awaited<ReturnType<typeof mountApp>>) => w.find('.slide[role="switch"]')

// passkeyBrowser makes the browser offer passkeys, and get answers as given.
function passkeyBrowser(get: (o?: { signal?: AbortSignal }) => Promise<unknown>) {
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

test('sign-in shows only its unit: no menubar or tabs, a dim MUSIC SHELF line, and a heading for screen readers', async () => {
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp()
  expect(w.find('.menubar').exists()).toBe(false)
  expect(w.find('.dock').exists()).toBe(false)
  expect(w.find('.plate').exists()).toBe(false)
  expect(w.find('h1.sr').text()).toBe('Sign in to Music Shelf')
  expect(w.find('.lcd-l.dim').text()).toBe('MUSIC SHELF')
  expect(w.findAll('.acct-blank')).toHaveLength(2)
  expect(lcd(w)).toEqual(['SIGNED OUT', 'PASSWORD'])
  expect(w.findAll('form button').map((b) => b.text())).toEqual(['Sign in'])
  w.unmount()
})

test('a browser without passkeys shows no slide and no key switch', async () => {
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp()
  expect(slide(w).exists()).toBe(false)
  expect(w.find('.keysw').exists()).toBe(false)
  w.unmount()
})

test('a 401 shows Shelf words on the display, empties the password and focuses it', async () => {
  stubApi({ '/api/auth/login': new Reply(401, { error: 'Incorrect credentials or credentials changed; sign in again' }), '/api/auth': signedOut })
  const w = await mountApp()
  await tryPassword(w)
  expect(lcd(w)).toEqual(['WRONG USERNAME', 'OR PASSWORD'])
  expect(w.find('.lcd').attributes('role')).toBe('alert')
  expect(w.find('.lcd').classes()).toContain('err')
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
  expect(w.find('.lcd').classes()).toContain('err')
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
  expect(w.find('.lcd').attributes('role')).toBe('status')
  expect(w.find('.lcd').classes()).not.toContain('err')
  release(); await flushPromises()
  w.unmount()
})

test('the sign-in unit follows the Day theme', async () => {
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp(true)
  expect(w.find('.acct-desk').classes()).toContain('day')
  expect(w.find('.acct-desk').text()).toContain('MUSIC SHELF')
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

const noAccounts = { provider: 'simple', account: null, setup_needed: true, setup_ui: true }
async function fill(w: Awaited<ReturnType<typeof mountApp>>, password: string, again: string) {
  await w.find('input[name="username"]').setValue('alice')
  await w.find('input[name="password"]').setValue(password)
  await w.find('input[name="password2"]').setValue(again)
  await w.find('form').trigger('submit')
  await flushPromises()
}

test('with no accounts and setup on, the unit creates the first one with no passkey slide', async () => {
  stubApi({ '/api/auth': noAccounts })
  const w = await mountApp()
  expect(lcd(w)).toEqual(['NO ACCOUNTS YET', 'CREATE THE FIRST ONE'])
  expect(w.find('h1').text()).toBe('Create the first Music Shelf account')
  expect(w.findAll('label').map((l) => l.text())).toEqual(['Username', 'Password', 'Repeat password'])
  expect(w.find('input[name="password"]').attributes('autocomplete')).toBe('new-password')
  expect(w.find('button[type="submit"]').text()).toBe('Create account')
  expect(w.find('pre').exists()).toBe(false)
  expect(slide(w).exists()).toBe(false)
  w.unmount()
})

test('a short or repeated-wrong password turns the display red and sends nothing', async () => {
  const calls = stubApi({ '/api/auth': noAccounts })
  const w = await mountApp()
  await fill(w, 'short', 'short')
  expect(lcd(w)).toEqual(['PASSWORD TOO SHORT', '8 CHARACTERS OR MORE'])
  expect(w.find('.lcd').classes()).toContain('err')
  expect(w.find('.lcd').attributes('role')).toBe('alert')
  await fill(w, 'long enough', 'long enougb')
  expect(lcd(w)).toEqual(['PASSWORDS DIFFER', 'TYPE THEM AGAIN'])
  expect((w.find('input[name="password2"]').element as HTMLInputElement).value).toBe('')
  expect(calls.some((c) => c.url.includes('/setup'))).toBe(false)
  w.unmount()
})

test('the new account powers up and opens the app', async () => {
  reduceMotion(true)
  const calls = stubApi({ '/api/auth/setup': alice, '/api/auth': noAccounts, ...appRoutes })
  const w = await mountApp()
  await fill(w, 'musicshelf', 'musicshelf')
  const call = calls.find((c) => c.url === '/api/auth/setup')!
  expect(JSON.parse(String(call.init?.body))).toEqual({ username: 'alice', password: 'musicshelf' })
  expect(w.find('.acct-desk').exists()).toBe(false)
  w.unmount()
})

test('when someone created the first account a moment earlier, the unit says so and turns to sign-in', async () => {
  let created = false
  stubApi({ '/api/auth/setup': () => { created = true; return new Reply(409, { error: 'an account already exists; sign in' }) }, '/api/auth': () => (created ? signedOut : noAccounts) })
  const w = await mountApp()
  await fill(w, 'musicshelf', 'musicshelf')
  expect(lcd(w)).toEqual(['USERNAME TAKEN', 'SIGN IN INSTEAD'])
  expect(w.find('input[name="password2"]').exists()).toBe(false)
  expect(w.find('button[type="submit"]').text()).toBe('Sign in')
  expect((w.find('input[name="username"]').element as HTMLInputElement).value).toBe('alice')
  w.unmount()
})

test('with setup off, the unit names the command instead of the form', async () => {
  stubApi({ '/api/auth': { ...noAccounts, setup_ui: false } })
  const w = await mountApp()
  expect(lcd(w)).toEqual(['NO ACCOUNTS', 'ADD ONE ON THE SERVER'])
  expect(w.find('pre').text()).toContain('shelf manage users add <username>')
  expect(w.find('form').exists()).toBe(false)
  w.unmount()
})

test('after sign-in the lamps in the unit light 260 ms apart and the clock starts, then the app mounts', async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'setInterval'] })
  stubApi({ '/api/auth/login': alice, '/api/auth': signedOut, ...appRoutes })
  const w = await mountApp(false, '/search')
  await tryPassword(w)
  expect(lcd(w)).toEqual(['HELLO, ALICE', 'OPENING SEARCH'])
  expect(w.find('form').exists()).toBe(false)
  expect(w.find('.acct-lamps .vfd').text()).toMatch(/^\d\d:\d\d$/)
  expect(w.findAll('.acct-lamps .led.ok')).toHaveLength(0)
  await vi.advanceTimersByTimeAsync(260)
  expect(w.findAll('.acct-lamps .led.ok')).toHaveLength(1)
  await vi.advanceTimersByTimeAsync(520)
  expect(w.findAll('.acct-lamps .led.ok')).toHaveLength(3)
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

test('a canceled passkey springs the slide back to OFF and turns the display amber', async () => {
  passkeyBrowser(() => Promise.reject(new DOMException('canceled', 'NotAllowedError')))
  stubApi({ '/api/auth/passkey/begin': { publicKey: { challenge: 'AAAA' } }, '/api/auth': signedOut })
  const w = await mountApp()
  expect(lcd(w)).toEqual(['SIGNED OUT', 'PASSWORD OR PASSKEY'])
  expect(slide(w).attributes('aria-label')).toBe('Sign in with a passkey')
  expect(w.findAll('form button').map((b) => b.text())).toEqual(['Sign in'])
  await slide(w).trigger('click')
  await flushPromises()
  expect(lcd(w)).toEqual(['PASSKEY CANCELED', 'USE THE PASSWORD'])
  expect(w.find('.lcd').classes()).toContain('warn')
  expect(w.find('.lcd').attributes('role')).toBe('status')
  expect(slide(w).attributes('aria-checked')).toBe('false')
  expect(w.find('.keysw-bow').classes()).not.toContain('on')
  w.unmount()
})

test('the slide stays at ON while the passkey is asked for, and sliding back cancels the request', async () => {
  let signal: AbortSignal | undefined
  passkeyBrowser((o?: { signal?: AbortSignal }) => new Promise((_, reject) => {
    signal = o?.signal
    signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
  }))
  stubApi({ '/api/auth/passkey/begin': { publicKey: { challenge: 'AAAA' } }, '/api/auth': signedOut })
  const w = await mountApp()
  await slide(w).trigger('click')
  await flushPromises()
  expect(lcd(w)).toEqual(['WAITING FOR', 'YOUR PASSKEY'])
  expect(slide(w).attributes('aria-checked')).toBe('true')
  expect(w.find('.keysw-bow').classes()).not.toContain('on')
  expect(w.find('button[type="submit"]').attributes('disabled')).toBeDefined()
  await slide(w).trigger('click')
  await flushPromises()
  expect(signal?.aborted).toBe(true)
  expect(lcd(w)).toEqual(['PASSKEY CANCELED', 'USE THE PASSWORD'])
  expect(slide(w).attributes('aria-checked')).toBe('false')
  w.unmount()
})

test('a 24px drag flips the slide, and a shorter one does not', async () => {
  const get = vi.fn(() => Promise.reject(new DOMException('canceled', 'NotAllowedError')))
  passkeyBrowser(get)
  stubApi({ '/api/auth/passkey/begin': { publicKey: { challenge: 'AAAA' } }, '/api/auth': signedOut })
  const w = await mountApp()
  // jsdom has no PointerEvent, so the test sends plain events that carry clientX.
  const pointer = (type: string, clientX = 0) => slide(w).element.dispatchEvent(Object.assign(new Event(type), { clientX }))
  pointer('pointerdown', 100); pointer('pointermove', 120); pointer('pointerup')
  await flushPromises()
  expect(get).not.toHaveBeenCalled()
  pointer('pointerdown', 100); pointer('pointermove', 125)
  // The click that ends the drag doesn't flip it back.
  await slide(w).trigger('click')
  await flushPromises()
  expect(get).toHaveBeenCalledTimes(1)
  w.unmount()
})

test('pressing the key switch asks for a passkey, as the slide does', async () => {
  const get = vi.fn(() => Promise.reject(new DOMException('canceled', 'NotAllowedError')))
  passkeyBrowser(get)
  stubApi({ '/api/auth/passkey/begin': { publicKey: { challenge: 'AAAA' } }, '/api/auth': signedOut })
  const w = await mountApp()
  expect(w.find('.keysw').attributes('width')).toBe('150')
  await w.find('.keysw[data-press]').trigger('click')
  await flushPromises()
  expect(get).toHaveBeenCalledTimes(1)
  expect(lcd(w)).toEqual(['PASSKEY CANCELED', 'USE THE PASSWORD'])
  w.unmount()
})

test('a passkey sign-in turns the key to ON', async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'setInterval'] })
  passkeyBrowser(() => Promise.resolve(Object.assign(Object.create(null), { id: 'k', rawId: new ArrayBuffer(1), type: 'public-key', response: { clientDataJSON: new ArrayBuffer(1), authenticatorData: new ArrayBuffer(1), signature: new ArrayBuffer(1), userHandle: null }, getClientExtensionResults: () => ({}) })))
  stubApi({ '/api/auth/passkey/begin': { publicKey: { challenge: 'AAAA' } }, '/api/auth/passkey/finish': alice, '/api/auth': signedOut, ...appRoutes })
  const w = await mountApp()
  await slide(w).trigger('click')
  await flushPromises()
  expect(lcd(w)).toEqual(['HELLO, ALICE', 'OPENING SEARCH'])
  expect(w.find('.keysw-bow').classes()).toContain('on')
  expect(slide(w).attributes('aria-checked')).toBe('true')
  expect(slide(w).attributes('aria-disabled')).toBe('true')
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
  expect(dialog.find('.lcd').classes()).toContain('warn')
  expect(dialog.find('[role="status"]').text()).toContain('SESSION ENDED')
  expect(dialog.find('[role="status"]').text()).toContain('2 EDITS KEPT')
  const user = dialog.find('input[name="username"]')
  expect((user.element as HTMLInputElement).value).toBe('alice')
  expect(user.attributes('readonly')).toBeDefined()
  expect(dialog.find('p').exists()).toBe(false)
  expect(dialog.findAll('.key').map((b) => b.text())).toEqual(['Sign in'])
  await dialog.findAll('button').find((b) => b.text() === 'Not alice?')!.trigger('click')
  expect((user.element as HTMLInputElement).value).toBe('')
  expect(user.attributes('readonly')).toBeUndefined()
  await user.setValue('bob')
  await dialog.find('input[name="password"]').setValue('wrong')
  await dialog.find('form').trigger('submit'); await flushPromises()
  expect(dialog.find('[role="alert"]').text()).toContain('WRONG USERNAME')
  w.unmount()
})

// phoneWith gives the phone layout and a visual viewport the keyboard covers by the given height.
function phoneWith(covered: number) {
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('max-width'), addEventListener() {}, removeEventListener() {} }))
  vi.stubGlobal('innerHeight', 844)
  vi.stubGlobal('visualViewport', { height: 844 - covered, offsetTop: 0, addEventListener() {}, removeEventListener() {} })
}

test('the phone centers the unit, with the slide in its own row under the form and no key switch', async () => {
  phoneWith(0)
  passkeyBrowser(() => Promise.reject(new DOMException('canceled', 'NotAllowedError')))
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp()
  const stack = w.find('.acct-stack')
  expect(stack.attributes('data-keyboard')).toBeUndefined()
  expect(stack.attributes('style')).toContain('bottom: 10px')
  expect(w.find('.tb').exists()).toBe(false)
  expect(w.find('.keysw').exists()).toBe(false)
  expect(w.find('.acct-pk .slide.row').exists()).toBe(true)
  w.unmount()
})

test('while the keyboard is open the phone unit moves to the top, above the keys', async () => {
  phoneWith(300)
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp()
  const stack = w.find('.acct-stack')
  expect(stack.attributes('data-keyboard')).toBe('true')
  expect(stack.attributes('style')).toContain('bottom: 310px')
  w.unmount()
})

test('when the open keyboard leaves too little room, the phone body scrolls Sign in into view', async () => {
  phoneWith(300)
  stubApi({ '/api/auth': signedOut })
  const w = await mountApp()
  const body = w.find('.acct-body').element as HTMLElement
  let top = 0
  Object.defineProperty(body, 'scrollTop', { get: () => top, set: (v: number) => { top = v } })
  body.getBoundingClientRect = () => ({ top: 11, bottom: 320 }) as DOMRect
  const key = w.find('form button[type="submit"]').element as HTMLElement
  key.getBoundingClientRect = () => ({ top: 298 - top, bottom: 352 - top }) as DOMRect
  await w.find('input[name="password"]').trigger('focusin')
  expect(top).toBe(42)
  await w.find('input[name="username"]').trigger('focusin')
  expect(top).toBe(42)
  w.unmount()
})

test('Sign in again has a Passkey slide under Sign in, and a canceled passkey turns its display amber', async () => {
  passkeyBrowser(() => Promise.reject(new DOMException('canceled', 'NotAllowedError')))
  stubApi({ '/api/auth': alice, '/api/auth/passkey/begin': { publicKey: { challenge: 'AAAA' } }, ...appRoutes })
  const w = await mountApp()
  useAuth().expired(); await flushPromises()
  const dialog = w.find('dialog')
  const sw = dialog.find('.account-pk .slide.row')
  expect(sw.attributes('aria-checked')).toBe('false')
  await sw.trigger('click'); await flushPromises()
  expect(dialog.find('.lcd').text()).toContain('PASSKEY CANCELED')
  expect(dialog.find('.lcd').classes()).toContain('warn')
  expect(sw.attributes('aria-checked')).toBe('false')
  w.unmount()
})
