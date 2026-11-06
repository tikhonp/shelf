import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import { createAppRouter } from '../router'
import { useAuth } from '../stores/auth'
import { FakeEventSource, list, stubApi } from '../testing/api'

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
