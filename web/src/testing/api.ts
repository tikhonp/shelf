import { vi } from 'vitest'
import type { Download, Item, Source } from '../api/types'

type Answer = unknown | ((url: string, init?: RequestInit) => unknown)

export class Reply {
  constructor(
    readonly status: number,
    readonly body: unknown,
  ) {}
}

// stubApi answers fetch from a list of path prefixes, longest first, and keeps the calls.
export function stubApi(routes: Record<string, Answer>) {
  routes = { '/api/auth': { provider: 'none', account: null, setup_needed: false }, ...routes }
  const calls: { url: string; init?: RequestInit }[] = []
  const paths = Object.keys(routes).sort((a, b) => b.length - a.length)
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    const path = paths.find((p) => url.startsWith(p))
    if (path === undefined) return respond(new Reply(404, { error: `no route for ${url}` }))
    const answer = routes[path]
    return respond(typeof answer === 'function' ? await (answer as (u: string, i?: RequestInit) => unknown)(url, init) : answer)
  })
  return calls
}

function respond(answer: unknown) {
  const r = answer instanceof Reply ? answer : new Reply(200, answer)
  return { ok: r.status < 400, status: r.status, statusText: 'Status', json: async () => r.body }
}

// FakeEventSource stands in for the browser's, and tests drive it by hand.
export class FakeEventSource {
  static all: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  private listeners: Record<string, ((m: MessageEvent<string>) => void)[]> = {}

  constructor(readonly url: string) {
    FakeEventSource.all.push(this)
  }
  addEventListener(name: string, fn: (m: MessageEvent<string>) => void) {
    ;(this.listeners[name] ??= []).push(fn)
  }
  close() {
    this.closed = true
  }
  emit(name: string, data: unknown) {
    for (const fn of this.listeners[name] ?? []) fn({ data: JSON.stringify(data) } as MessageEvent<string>)
  }
}

export function source(id: string, name: string, over: Partial<Source> = {}): Source {
  return {
    id,
    name,
    enabled: true,
    capabilities: { links: true, search: ['album', 'track', 'artist', 'playlist'], resolve: true, download: true },
    formats: [
      { id: `${id}:hi`, label: 'Hi', description: 'FLAC up to 24-bit 96 kHz' },
      { id: `${id}:max`, label: 'Max', description: 'FLAC', default: true },
    ],
    health: { ok: true },
    ...over,
  }
}

export function item(src: string, id: string, over: Partial<Item> = {}): Item {
  return { ref: { source: src, kind: 'album', id }, title: id, quality: {}, ...over }
}

export function download(id: number, over: Partial<Download> = {}): Download {
  return {
    id,
    parent_id: null,
    source: 'qobuz',
    kind: 'album',
    item_id: `album-${id}`,
    url: '',
    format: 'qobuz:max',
    title: `Album ${id}`,
    artist: 'Artist',
    cover_url: '',
    status: 'queued',
    error: '',
    tracks_total: 0,
    tracks_done: 0,
    tracks_failed: 0,
    bytes: 0,
    folder: '',
    origin: 'web',
    origin_user: '',
    created_at: '2026-11-04T10:00:00Z',
    started_at: null,
    finished_at: null,
    ...over,
  }
}

export const page = (items: Item[], total = items.length) => ({ items, total, limit: 20, offset: 0 })
export const list = (downloads: Download[], next: number | null = null) => ({ downloads, next })
