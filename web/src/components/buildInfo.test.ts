import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { Reply, stubApi } from '../testing/api'
import BuildInfo from './BuildInfo.vue'

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

test('shows the version the server was built with', async () => {
  stubApi({ '/api/version': { version: 'v1.0.1' } })
  const w = mount(BuildInfo)
  await flushPromises()
  expect(w.text()).toContain('Shelf')
  expect(w.text()).toContain('VERSION V1.0.1')
})

test('says so when the version does not answer', async () => {
  stubApi({ '/api/version': new Reply(500, { error: 'boom' }) })
  const w = mount(BuildInfo)
  await flushPromises()
  expect(w.text()).toContain("VERSION DOESN'T ANSWER")
})
