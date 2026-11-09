import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import AccountSettings from './AccountSettings.vue'
import { createAppRouter } from '../router'
import { useAuth } from '../stores/auth'
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
  expect(w.text()).toContain('Sign in to Shelf')
  expect(w.find('.menubar').exists()).toBe(false)
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
  expect(w.text()).not.toContain('Confirm password for passkey changes')
  expect(w.text()).toContain('Current password')
  expect(w.text()).toContain('New password')
  expect(w.text()).toContain('New again')
  expect(w.text()).toContain('Change password')
  await w.findAll('button').find((b) => b.text() === 'Remove')!.trigger('click')
  await flushPromises()
  expect(w.find('.note').text()).toContain('Remove iPhone?')
  await w.find('.note input').setValue('secret password')
  await w.find('form.note').trigger('submit')
  await flushPromises()
  expect(calls.filter((c) => c.url.includes('/passkeys/one') || c.url.includes('/verify')).map((c) => c.url)).toEqual(['/api/auth/passkeys/one', '/api/auth/verify', '/api/auth/passkeys/one'])
  expect(JSON.parse(calls.find((c) => c.url.endsWith('/verify'))!.init!.body as string)).toEqual({ password: 'secret password' })
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
