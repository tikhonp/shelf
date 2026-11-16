import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, test } from 'vitest'
import { nextTick } from 'vue'
import LibGuide from './LibGuide.vue'
import { useInbox } from '../stores/inbox'

beforeEach(() => setActivePinia(createPinia()))

test('the guide explains a single with guests', () => {
  const w = mount(LibGuide)
  expect(w.text()).toContain('For a single with guests, Album Artist is the main artist alone')
})

test('the guide mentions agents only while the Inbox is enabled', async () => {
  const inbox = useInbox()
  const w = mount(LibGuide)
  expect(w.text()).not.toContain('An agent can research tags')
  inbox.enabled = true
  await nextTick()
  expect(w.text()).toContain('An agent can research tags, such as performers, and propose them.')
})
