import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { afterEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import App from './App.vue'
import { createAppRouter } from './router'

afterEach(() => {
  vi.unstubAllGlobals()
})

async function mountAt(path: string) {
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  const wrapper = mount(App, { global: { plugins: [createPinia(), router] } })
  await flushPromises()
  return wrapper
}

function answer(ok: boolean, body: unknown) {
  return vi.fn().mockResolvedValue({ ok, statusText: ok ? 'OK' : 'Bad Gateway', json: async () => body })
}

test('the page shows the version from /api/version', async () => {
  const fetch = answer(true, { version: 'main-abc1234' })
  vi.stubGlobal('fetch', fetch)
  const wrapper = await mountAt('/')
  expect(fetch).toHaveBeenCalledWith('/api/version')
  expect(wrapper.text()).toContain('Music Shelf')
  expect(wrapper.text()).toContain('Version main-abc1234')
})

test('the page says so when the version cannot be read', async () => {
  vi.stubGlobal('fetch', answer(false, { error: 'Bad Gateway' }))
  const wrapper = await mountAt('/')
  expect(wrapper.text()).toContain("Couldn't read the version")
})

test('any path shows the page', async () => {
  vi.stubGlobal('fetch', answer(true, { version: 'dev' }))
  const wrapper = await mountAt('/downloads/42')
  expect(wrapper.text()).toContain('Version dev')
})
