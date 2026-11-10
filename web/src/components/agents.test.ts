import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Connection } from '../api/agents'
import AgentsPanel from './AgentsPanel.vue'
import { useInbox } from '../stores/inbox'
import { stubApi } from '../testing/api'

const hour = 3600_000
const at = (ago: number) => new Date(Date.now() - ago).toISOString()
const conn = (id: number, over: Partial<Connection> = {}): Connection => ({
  id, name: `Agent ${id}`, operations: ['library.read', 'history.read'], created_by: 'CLI', created_at: '2026-11-09T10:00:00Z',
  last_used_at: null, revoked_at: null, revoke_reason: '', revoked_by: '', last_run_at: null, proposals: 0, rules: 0, ...over,
})
const setup = { api_base: 'https://x/api/agent/v1', review_base: 'https://x', prompt: 'NO SECRET', operations: [{ name: 'library.read', summary: '' }, { name: 'covers.upload', summary: '' }] }
const info = { pending: 5, changes_requested: 0, connections: 1, accounts: 'simple', can_create_connections: true }

beforeEach(() => {
  setActivePinia(createPinia())
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
})

async function mountPanel(routes: Record<string, unknown>) {
  const calls = stubApi({ '/api/inbox': info, '/api/inbox/setup': setup, ...routes })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
  await useInbox().detect()
  const w = mount(AgentsPanel, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return { w, calls }
}
const unit = (w: Awaited<ReturnType<typeof mountPanel>>['w'], name: string) => w.find(`section[aria-label="${name}"]`)

test('each connection has a unit with its facts, and the Inbox lamp glows while proposals wait', async () => {
  const { w } = await mountPanel({ '/api/inbox/connections': [
    conn(1, { name: 'Claude at home', last_run_at: at(3 * 60_000), proposals: 3, rules: 1 }),
    conn(2, { name: 'Old laptop', revoked_at: at(49 * hour), revoked_by: 'tikhon', revoke_reason: 'user', proposals: 0 }),
    conn(3, { name: 'New one' }),
  ] })
  expect(unit(w, 'Agents').text()).toContain('2 CONNECTED')
  expect(unit(w, 'Agents').text()).toContain('3 PROPOSALS PENDING')
  expect(w.find('.msgbtn').attributes('data-lit')).toBe('')
  expect(w.find('.msgbtn').attributes('aria-label')).toBe('Open the Inbox, proposals wait')
  const home = unit(w, 'Claude at home')
  expect(home.find('.sub').text()).toBe('Added by CLI on 9 Nov')
  expect(home.text()).toContain('LAST RUN 3 MINUTES AGO')
  expect(home.text()).toContain('3 PROPOSALS · 1 RULE')
  expect(home.findAll('.stk').map((s) => s.text())).toEqual(['Read the library', 'History'])
  expect(unit(w, 'New one').text()).toContain('NOT USED YET')
  expect(unit(w, 'New one').text()).toContain('0 PROPOSALS')
  const old = unit(w, 'Old laptop')
  expect(old.classes()).toContain('gone')
  expect(old.find('.plate .sub').text()).toBe('Revoked by tikhon')
  expect(old.text()).toContain('REVOKED 2 DAYS AGO')
  expect(old.find('button.jackbtn').exists()).toBe(false)
  expect(old.find('[data-jack]').attributes('data-jack')).toBe('out')
  // Revoked units follow the active ones, and Connect comes last.
  expect(w.findAll('section').map((s) => s.attributes('aria-label'))).toEqual(['Agents', 'Claude at home', 'New one', 'Old laptop', 'Connect'])
  expect(w.text()).not.toContain('NO SECRET')
  w.unmount()
})

test('without waiting proposals the Inbox lamp is dark', async () => {
  const { w } = await mountPanel({ '/api/inbox/connections': [conn(1)] })
  expect(w.find('.msgbtn').attributes('data-lit')).toBeUndefined()
  expect(w.find('.msgbtn').attributes('aria-label')).toBe('Open the Inbox')
  w.unmount()
})

test('Revoke pulls the plug half out and asks, then pulls it out', async () => {
  let revoked = false
  const { w, calls } = await mountPanel({
    '/api/inbox/connections/1/revoke': () => { revoked = true; return {} },
    '/api/inbox/connections': () => [conn(1, { name: 'Claude at home', ...(revoked ? { revoked_at: at(1000), revoked_by: 'alice' } : {}) })],
  })
  const home = () => unit(w, 'Claude at home')
  await home().find('button.jackbtn').trigger('click')
  expect(home().find('[data-jack]').attributes('data-jack')).toBe('asking')
  expect(home().text()).toContain('Revoke Claude at home? Its token stops working at once.')
  await home().findAll('button').find((b) => b.text() === 'Cancel')!.trigger('click')
  expect(home().find('[data-jack]').attributes('data-jack')).toBe('in')
  expect(calls.some((c) => c.url.endsWith('/revoke'))).toBe(false)
  await home().find('button.jackbtn').trigger('click')
  await home().findAll('button.key').find((b) => b.text() === 'Revoke')!.trigger('click')
  await flushPromises()
  expect(home().find('[data-jack]').attributes('data-jack')).toBe('out')
  expect(home().find('.led').classes()).toContain('off')
  expect(home().text()).toContain('REVOKED JUST NOW')
  expect(home().find('.plate .sub').text()).toBe('Revoked by alice')
  w.unmount()
})

test('a new prompt locks the form, and Done asks when nothing was copied', async () => {
  vi.stubGlobal('navigator', { clipboard: { writeText: async () => {} } })
  const created = { connection: conn(7, { name: 'Claude on the laptop' }), token: 'shelf_agent_SECRET', prompt: 'PROMPT shelf_agent_SECRET' }
  const { w, calls } = await mountPanel({
    '/api/inbox/connections': (_u: string, init?: RequestInit) => (init?.method === 'POST' ? created : []),
  })
  const connect = unit(w, 'Connect')
  expect(connect.find('button[type="submit"]').classes()).toContain('sun')
  expect(connect.text()).not.toContain('No agent can accept')
  await connect.find('input').setValue('Claude on the laptop')
  await connect.findAll('.rocker')[1]!.trigger('click')
  await connect.find('form').trigger('submit')
  await flushPromises()
  const post = calls.find((c) => c.init?.method === 'POST')!
  expect(JSON.parse(post.init!.body as string)).toEqual({ name: 'Claude on the laptop', operations: ['library.read'] })

  expect(w.text()).toContain('Prompt for Claude on the laptop')
  expect(w.find('input').attributes('readonly')).toBeDefined()
  expect(w.findAll('.rocker').every((r) => r.attributes('aria-disabled') === 'true')).toBe(true)
  await w.findAll('.rocker')[1]!.trigger('click')
  expect(w.findAll('.rocker')[1]!.attributes('aria-checked')).toBe('false')
  const submit = w.find('button[type="submit"]')
  expect(submit.attributes('disabled')).toBeDefined()
  expect(submit.classes()).not.toContain('sun')
  expect(w.findAll('.key.sun').map((b) => b.text())).toEqual(['Copy prompt'])

  const button = (text: string) => w.findAll('button').find((b) => b.text() === text)
  await button('Done')!.trigger('click')
  expect(w.text()).toContain('Nothing copied yet, and Shelf shows it only once.')
  expect(w.text()).toContain('shelf_agent_SECRET')
  await button('Copy prompt')!.trigger('click')
  await flushPromises()
  expect(w.text()).toContain('Prompt copied.')
  expect(w.text()).not.toContain('Nothing copied yet')
  await button('Done')!.trigger('click')
  expect(w.text()).not.toContain('shelf_agent_SECRET')
  expect(w.find('input').attributes('readonly')).toBeUndefined()
  expect((w.find('input').element as HTMLInputElement).value).toBe('')
  expect(w.findAll('.rocker').map((r) => r.attributes('aria-checked'))).toEqual(['true', 'true'])
  w.unmount()
})

test('Close anyway drops the prompt without a copy', async () => {
  const created = { connection: conn(7), token: 'shelf_agent_SECRET', prompt: 'PROMPT' }
  const { w } = await mountPanel({ '/api/inbox/connections': (_u: string, init?: RequestInit) => (init?.method === 'POST' ? created : []) })
  await w.find('input').setValue('Agent 7')
  await w.find('form').trigger('submit')
  await flushPromises()
  await w.findAll('button').find((b) => b.text() === 'Done')!.trigger('click')
  await w.findAll('button').find((b) => b.text() === 'Close anyway')!.trigger('click')
  expect(w.text()).not.toContain('shelf_agent_SECRET')
  w.unmount()
})

test('without Shelf sign-in Connect names the CLI command', async () => {
  const { w } = await mountPanel({ '/api/inbox': { ...info, accounts: 'none', can_create_connections: false }, '/api/inbox/connections': [] })
  expect(unit(w, 'Connect').text()).toContain('shelf manage agents add <name>')
  expect(unit(w, 'Connect').find('form').exists()).toBe(false)
  w.unmount()
})
