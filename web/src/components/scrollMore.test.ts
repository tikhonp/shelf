import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import ScrollMore from './ScrollMore.vue'

// A stand-in for IntersectionObserver. A new observer reports at once whether
// the end shows, as the browser's does, and scroll reports a change.
let showing = false
let observers: { cb: IntersectionObserverCallback; on: boolean }[] = []

class FakeObserver {
  private me: { cb: IntersectionObserverCallback; on: boolean }
  constructor(cb: IntersectionObserverCallback) {
    this.me = { cb, on: true }
    observers.push(this.me)
  }
  observe() {
    queueMicrotask(() => this.report())
  }
  disconnect() {
    this.me.on = false
  }
  report() {
    if (this.me.on) this.me.cb([{ isIntersecting: showing } as IntersectionObserverEntry], this as unknown as IntersectionObserver)
  }
}

function scroll(into: boolean) {
  showing = into
  for (const o of observers) if (o.on) o.cb([{ isIntersecting: into } as IntersectionObserverEntry], {} as IntersectionObserver)
}

beforeEach(() => {
  showing = false
  observers = []
  vi.stubGlobal('IntersectionObserver', FakeObserver)
})
afterEach(() => vi.unstubAllGlobals())

// A list that grows by page rows each read, up to total. A long page pushes
// the end out of view.
async function mountList(page: number, total = Infinity, long = false) {
  const state = { count: page }
  const load = vi.fn()
  const w = mount(ScrollMore, { props: { load, count: state.count } })
  load.mockImplementation(async () => {
    state.count = Math.min(total, state.count + page)
    if (long) showing = false
    await w.setProps({ count: state.count })
  })
  await flushPromises()
  return { w, load, state }
}

test('reads nothing while the end of the list is out of view', async () => {
  const { load } = await mountList(10)
  expect(load).not.toHaveBeenCalled()
})

test('reads the next page when the end scrolls into view', async () => {
  const { w, load } = await mountList(10, Infinity, true)
  scroll(true)
  await flushPromises()
  expect(load).toHaveBeenCalledTimes(1)
  expect(w.find('button').exists()).toBe(false)
})

test('keeps reading while a short page leaves the end in view', async () => {
  showing = true
  const { load, state } = await mountList(10, 30)
  await flushPromises()
  expect(state.count).toBe(30)
  // Two pages filled the list, and a third added nothing.
  expect(load).toHaveBeenCalledTimes(3)
})

test('a page that adds no rows stops the reads and shows the button', async () => {
  const load = vi.fn(async () => {
    throw new Error('boom')
  })
  const w = mount(ScrollMore, { props: { load, count: 10 } })
  scroll(true)
  await flushPromises()
  scroll(false)
  scroll(true)
  await flushPromises()
  expect(load).toHaveBeenCalledTimes(1)
  const button = w.get('button')
  expect(button.text()).toBe('Load more')
  await button.trigger('click')
  await flushPromises()
  expect(load).toHaveBeenCalledTimes(2)
})

test('waits for a read the store has running', async () => {
  const load = vi.fn()
  const w = mount(ScrollMore, { props: { load, count: 10, busy: true } })
  scroll(true)
  await flushPromises()
  expect(load).not.toHaveBeenCalled()
  expect(w.text()).toContain('Loading more...')
  await w.setProps({ busy: false })
  await flushPromises()
  expect(load).toHaveBeenCalledTimes(1)
})

test('shows the button where the browser has no IntersectionObserver', async () => {
  vi.stubGlobal('IntersectionObserver', undefined)
  const load = vi.fn()
  const w = mount(ScrollMore, { props: { load, count: 10 } })
  await w.get('button').trigger('click')
  expect(load).toHaveBeenCalledTimes(1)
})
