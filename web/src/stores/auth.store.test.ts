import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { accountCall, useAuth } from './auth'
import { Reply, stubApi } from '../testing/api'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())
test('none mode opens the app without account controls', async () => {
  stubApi({})
  const auth = useAuth()
  expect(auth.mounted).toBe(false)
  await auth.check()
  expect(auth.mounted).toBe(true)
  expect(auth.locked).toBe(false)
  expect(auth.provider).toBe('none')
})
test('simple mode stays closed until the server verifies credentials', async () => {
  stubApi({ '/api/auth': { provider: 'simple', account: null, setup_needed: false }, '/api/auth/login': new Reply(401, { error: 'Incorrect credentials' }) })
  const auth = useAuth()
  await auth.check()
  expect(auth.locked).toBe(true)
  expect(auth.mounted).toBe(false)
  await expect(auth.login('Alice', 'wrong')).rejects.toThrow('Incorrect credentials')
  expect(auth.mounted).toBe(false)
})
test('expired access keeps mounted state and only the same account resumes', async () => {
  const signed = { provider: 'simple', account: { id: 17, username: 'Alice' }, setup_needed: false }
  stubApi({ '/api/auth': signed, '/api/auth/login': signed })
  const auth = useAuth()
  await auth.check()
  auth.expired()
  expect(auth.locked).toBe(true)
  expect(auth.mounted).toBe(true)
  expect(auth.previousID).toBe(17)
  await auth.login('Alice', 'a sufficiently long password')
  expect(auth.locked).toBe(false)
  expect(auth.account?.id).toBe(17)
})
test('status checks coalesce and network failure never opens the app', async () => {
  let release!: () => void
  const done = new Promise<void>((resolve) => { release = resolve })
  let calls = 0
  vi.stubGlobal('fetch', async () => { calls++; await done; throw new Error('offline') })
  const auth = useAuth()
  const a = auth.check().catch(() => {})
  const b = auth.check().catch(() => {})
  release(); await Promise.all([a, b])
  expect(calls).toBe(1)
  expect(auth.ready).toBe(false)
  expect(auth.mounted).toBe(false)
  expect(auth.error).toBe('offline')
})
test('account requests send no credentials in a URL or browser storage', async () => {
  const calls = stubApi({ '/api/auth/login': { provider: 'simple', account: { id: 1, username: 'Alice' }, setup_needed: false } })
  await accountCall('/login', { username: 'Alice', password: 'a sufficiently long password' })
  expect(calls[0]?.url).toBe('/api/auth/login')
  expect(calls[0]?.init?.cache).toBe('no-store')
})

test('another account forces a full navigation instead of reusing staged edits', async () => {
  stubApi({ '/api/auth': { provider: 'simple', account: { id: 17, username: 'Alice' }, setup_needed: false } })
  const auth = useAuth(); await auth.check(); auth.expired()
  const reload = vi.fn()
  vi.stubGlobal('window', { location: { reload, pathname: '/library/7', search: '?open=history', hash: '#track-2' } })
  auth.signedIn({ provider: 'simple', account: { id: 18, username: 'Bob' }, setup_needed: false })
  expect(reload).toHaveBeenCalledOnce()
  expect(auth.locked).toBe(true)
})

test('a rejected account request checks whether the session expired', async () => {
  stubApi({ '/api/auth': { provider: 'simple', account: { id: 17, username: 'Alice' }, setup_needed: false } })
  const auth = useAuth(); await auth.check()
  stubApi({ '/api/auth': { provider: 'simple', account: null, setup_needed: false }, '/api/auth/passkeys': new Reply(401, { error: 'Sign in to Shelf' }) })
  await expect(accountCall('/passkeys', undefined, 'GET')).rejects.toThrow('Sign in to Shelf')
  await auth.check()
  expect(auth.locked).toBe(true)
})


test('a background account check also reloads after another tab switches accounts', async () => {
  stubApi({ '/api/auth': { provider: 'simple', account: { id: 17, username: 'Alice' }, setup_needed: false } })
  const auth = useAuth(); await auth.check(); auth.expired()
  const reload = vi.fn()
  vi.stubGlobal('window', { location: { reload } })
  stubApi({ '/api/auth': { provider: 'simple', account: { id: 18, username: 'Bob' }, setup_needed: false } })
  await auth.check()
  expect(reload).toHaveBeenCalledOnce()
  expect(auth.locked).toBe(true)
  expect(auth.previousID).toBe(17)
})
