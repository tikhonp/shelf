import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import AccountSettings from './AccountSettings.vue'
import { createAppRouter } from '../router'
import { useAuth } from '../stores/auth'
import { useEditor } from '../stores/editor'
import { FakeEventSource, Reply, list, stubApi } from '../testing/api'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('EventSource', FakeEventSource)
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function(this: HTMLDialogElement) { this.open = true } })
})
async function mountApp() {
  const pinia = createPinia(); setActivePinia(pinia)
  const router = createAppRouter(createMemoryHistory()); await router.push('/search')
  const w = mount(App, { global: { plugins: [pinia, router] } }); await flushPromises(); return w
}
test('logged-out startup makes no protected requests or event streams', async () => {
  const calls = stubApi({ '/api/auth': { provider: 'simple', account: null, setup_needed: false } })
  const w = await mountApp()
  expect(w.find('#acct-h').text()).toBe('Sign in to Music Shelf')
  expect(w.find('.menubar a').exists()).toBe(false)
  expect(calls.map((c) => c.url)).toEqual(['/api/auth'])
  expect(w.find('input[name="username"]').attributes('autocomplete')).toBe('username webauthn')
  expect(w.find('input[type="password"]').attributes('autocomplete')).toBe('current-password')
  w.unmount()
})
test('setup message has no registration form', async () => {
  stubApi({ '/api/auth': { provider: 'simple', account: null, setup_needed: true } })
  const w = await mountApp()
  expect(w.text()).toContain('shelf manage users add')
  expect(w.find('form').exists()).toBe(false)
  w.unmount()
})
test('401 keeps the mounted app inert until the same account returns', async () => {
  const signed = { provider: 'simple', account: { id: 1, username: 'Alice' }, setup_needed: false }
  stubApi({ '/api/auth': signed, '/api/downloads': list([]), '/api/sources': [], '/api/library/scan': { enabled: false }, '/api/telegram': { enabled: false }, '/api/search': { items: [], total: 0 } })
  const w = await mountApp()
  expect(w.find('.menubar').exists()).toBe(true)
  useAuth().expired(); await flushPromises()
  expect(w.find('[inert]').exists()).toBe(true)
  expect(w.find('.menubar').exists()).toBe(true)
  expect(w.find('dialog[open]').exists()).toBe(true)
  useAuth().signedIn(signed as Parameters<ReturnType<typeof useAuth>['signedIn']>[0]); await flushPromises()
  expect(w.find('dialog').exists()).toBe(false)
  expect(w.find('[inert]').exists()).toBe(false)
  w.unmount()
})

test('passkey removal asks for proof and retries once', async () => {
  let deletes = 0
  const calls = stubApi({
    '/api/auth/passkeys': (_url: string, init?: RequestInit) => init?.method === 'DELETE'
      ? (++deletes === 1 ? new Reply(403, { error: 'Confirm your password' }) : {})
      : { passkeys: [{ id: 'one', name: 'iPhone', created_at: '2026-01-01', last_used_at: null }] },
    '/api/auth/verify': {},
  })
  const auth = useAuth()
  auth.provider = 'simple'; auth.account = { id: 1, username: 'Alice' }
  const w = mount(AccountSettings)
  await flushPromises()
  expect(w.findAll('.lbl-f').map((l) => l.text())).toEqual(['Current', 'New', 'New, again'])
  expect(w.text()).not.toContain('Removing a passkey signs you out')
  expect(w.text()).not.toContain('sign in again')
  await w.findAll('button').find((b) => b.text() === 'Remove')!.trigger('click')
  await flushPromises()
  const ask = w.get('form.pk-ask')
  expect(ask.attributes('aria-label')).toBe('Remove iPhone?')
  expect(ask.text()).toContain('Removing a passkey signs you out here.')
  expect(ask.findAll('button').map((b) => b.text())).toEqual(['Cancel', 'Remove'])
  const go = () => ask.findAll('button').find((b) => b.text() === 'Remove')!
  expect(go().attributes('disabled')).toBeDefined()
  await ask.get('input').setValue('secret password')
  expect(go().attributes('disabled')).toBeUndefined()
  await ask.trigger('submit')
  await flushPromises()
  expect(calls.filter((c) => c.url.includes('/passkeys/one') || c.url.includes('/verify')).map((c) => c.url)).toEqual(['/api/auth/passkeys/one', '/api/auth/verify', '/api/auth/passkeys/one'])
  expect(JSON.parse(calls.find((c) => c.url.endsWith('/verify'))!.init!.body as string)).toEqual({ password: 'secret password' })
  w.unmount()
})

test('a device that already holds the passkey gets Shelf words, not the browser sentence', async () => {
  vi.stubGlobal('isSecureContext', true)
  vi.stubGlobal('PublicKeyCredential', class {})
  Object.defineProperty(navigator, 'credentials', { configurable: true, value: { create: () => Promise.reject(new DOMException('The user attempted to register an authenticator that contains one of the credentials already registered with the relying party.', 'InvalidStateError')) } })
  stubApi({ '/api/auth/passkeys': { passkeys: [] }, '/api/auth/passkeys/begin': { publicKey: { challenge: 'AAAA', user: { id: 'AAAA', name: 'alice', displayName: 'alice' }, excludeCredentials: [] } } })
  const auth = useAuth()
  auth.provider = 'simple'; auth.account = { id: 1, username: 'Alice' }
  const w = mount(AccountSettings)
  await flushPromises()
  await w.get('.pk-add input').setValue('Laptop')
  await w.get('.pk-add').trigger('submit')
  await flushPromises()
  expect(w.get('[role="alert"]').text()).toBe('This device already has a passkey for this account. Add one from another device.')
  w.unmount()
  vi.unstubAllGlobals()
})

test('the passkey confirm offers a passkey where the browser has them, and Cancel closes it', async () => {
  vi.stubGlobal('isSecureContext', true)
  vi.stubGlobal('PublicKeyCredential', class {})
  stubApi({ '/api/auth/passkeys': (_url: string, init?: RequestInit) => init?.method === 'DELETE' ? new Reply(403, { error: 'Confirm your password' }) : { passkeys: [{ id: 'one', name: 'iPhone', created_at: '2026-01-01', last_used_at: null }] } })
  const auth = useAuth()
  auth.provider = 'simple'; auth.account = { id: 1, username: 'Alice' }
  const w = mount(AccountSettings)
  await flushPromises()
  await w.findAll('button').find((b) => b.text() === 'Remove')!.trigger('click')
  await flushPromises()
  expect(w.get('form.pk-ask').findAll('button').map((b) => b.text())).toEqual(['Cancel', 'Confirm with a passkey', 'Remove'])
  await w.get('form.pk-ask button').trigger('click')
  expect(w.find('form.pk-ask').exists()).toBe(false)
  w.unmount()
  vi.unstubAllGlobals()
  vi.stubGlobal('EventSource', FakeEventSource)
})

test('an account without passkeys confirms adding one with its password only', async () => {
  vi.stubGlobal('isSecureContext', true)
  vi.stubGlobal('PublicKeyCredential', class {})
  stubApi({ '/api/auth/passkeys/begin': new Reply(403, { error: 'Confirm your password' }), '/api/auth/passkeys': { passkeys: [] } })
  const auth = useAuth()
  auth.provider = 'simple'; auth.account = { id: 1, username: 'Alice' }
  const w = mount(AccountSettings)
  await flushPromises()
  await w.get('.pk-add input').setValue('Laptop')
  await w.get('.pk-add').trigger('submit')
  await flushPromises()
  expect(w.get('form.pk-ask').findAll('button').map((b) => b.text())).toEqual(['Cancel', 'Add passkey'])
  w.unmount()
  vi.unstubAllGlobals()
  vi.stubGlobal('EventSource', FakeEventSource)
})

test('rename opens the name in place and saves it', async () => {
  const calls = stubApi({ '/api/auth/passkeys': { passkeys: [{ id: 'one', name: 'iPhone', created_at: '2026-01-01', last_used_at: '2026-01-04' }] }, '/api/auth/passkeys/one/name': {} })
  const auth = useAuth()
  auth.provider = 'simple'; auth.account = { id: 1, username: 'Alice' }
  const w = mount(AccountSettings)
  await flushPromises()
  expect(w.get('.passkey-row .sub').text()).toBe('Added 1 Jan · used 4 Jan')
  await w.findAll('button').find((b) => b.text() === 'Rename')!.trigger('click')
  await w.get('.pk-rename input').setValue('Old iPhone')
  await w.get('.pk-rename').trigger('submit')
  await flushPromises()
  expect(JSON.parse(calls.find((c) => c.url.endsWith('/name'))!.init!.body as string)).toEqual({ name: 'Old iPhone' })
  expect(w.find('.pk-rename').exists()).toBe(false)
  expect(w.get('.pk-name').text()).toBe('Old iPhone')
  expect(w.get('[role="status"]').text()).toBe('Passkey name saved.')
  w.unmount()
})

test('a failed password change shows its error in the Password unit', async () => {
  stubApi({ '/api/auth/passkeys': { passkeys: [] }, '/api/auth/password': new Reply(401, { error: 'Wrong current password' }), '/api/auth': { provider: 'simple', account: { id: 1, username: 'Alice' }, setup_needed: false } })
  const auth = useAuth()
  auth.provider = 'simple'; auth.account = { id: 1, username: 'Alice' }
  const w = mount(AccountSettings)
  await flushPromises()
  const inputs = w.findAll('input[type="password"]')
  for (const i of inputs) await i.setValue('long enough password')
  await w.findAll('form').at(-1)!.trigger('submit')
  await flushPromises()
  const alert = w.find('[aria-label="Password"] [role="alert"]')
  expect(alert.text()).toBe('Wrong current password')
  expect(alert.classes()).toContain('bad-text')
  expect(w.find('[aria-label="Passkeys"] [role="alert"]').exists()).toBe(false)
  w.unmount()
})

test.each([0, 1, 3])('the sign-out key protects %i staged edits', async (count) => {
  const calls = stubApi({ '/api/auth/passkeys': { passkeys: [] }, '/api/auth/logout': {} })
  const auth = useAuth()
  auth.provider = 'simple'; auth.account = { id: 1, username: 'Alice' }
  const editor = useEditor()
  editor.held = Object.fromEntries(Array.from({ length: count }, (_, i) => [i + 1, { stage: {}, cover: { upload: 'cover' } }]))
  const w = mount(AccountSettings)
  await flushPromises()
  const account = w.get('[aria-label="Account"]')
  const key = () => account.get('button.keyout')
  const bow = () => key().get('.keysw-bow')
  const logoutCalls = () => calls.filter((c) => c.url === '/api/auth/logout')
  expect(account.findAll('.lcd-l').map((l) => l.text())).toEqual(['ALICE', '0 PASSKEYS'])
  expect(key().attributes('aria-label')).toBe('Sign out of Shelf')
  expect(bow().classes()).toContain('on')
  await key().trigger('click')
  await flushPromises()
  if (count === 0) {
    expect(logoutCalls()).toHaveLength(1)
    expect(bow().classes()).not.toContain('on')
    expect(account.find('.ko-ask').exists()).toBe(false)
  } else {
    expect(logoutCalls()).toHaveLength(0)
    expect(bow().classes()).toContain('half')
    expect(account.get('.ko-ask strong').text()).toBe(`Sign out with ${count} staged ${count === 1 ? 'edit' : 'edits'}?`)
    await account.get('.ko-ask button').trigger('click')
    expect(account.find('.ko-ask').exists()).toBe(false)
    expect(bow().classes()).toEqual(expect.arrayContaining(['on']))
    expect(bow().classes()).not.toContain('half')
    expect(logoutCalls()).toHaveLength(0)
    expect(editor.kept).toBe(count)
    await key().trigger('click')
    const confirm = account.get('.ko-ask')
    expect(confirm.findAll('button').map((b) => b.text())).toEqual(['Keep editing', 'Sign out'])
    await confirm.findAll('button')[1]!.trigger('click')
    await flushPromises()
    expect(logoutCalls()).toHaveLength(1)
    expect(logoutCalls()[0]!.init?.method).toBe('POST')
    expect(bow().classes()).not.toContain('on')
  }
  w.unmount()
})

test('a failed sign-out turns the key back to ON', async () => {
  stubApi({ '/api/auth/passkeys': { passkeys: [] }, '/api/auth/logout': new Reply(500, { error: 'Database is locked' }) })
  const auth = useAuth()
  auth.provider = 'simple'; auth.account = { id: 1, username: 'Alice' }
  const w = mount(AccountSettings)
  await flushPromises()
  await w.get('button.keyout').trigger('click')
  await flushPromises()
  expect(w.get('.keysw-bow').classes()).toContain('on')
  expect(w.get('[aria-label="Account"] [role="alert"]').text()).toBe('Database is locked')
  w.unmount()
})
