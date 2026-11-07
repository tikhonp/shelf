import { defineStore } from 'pinia'
import { ref } from 'vue'
import { errorCode, listOf, type MemoryCorrection, type MemoryDoc, type MemoryLayer } from '../api/agents'
import { ApiError, del, errorText, get, post, put } from '../api/client'

export type LayerFilter = MemoryLayer
export type StatusFilter = 'active' | 'proposed' | 'deleted' | 'all'

// Result tells a form whether to keep the reviewer's text. stale means another write won and the document shows its current version.
export interface Result {
  ok: boolean
  error?: string
  stale?: boolean
}

const isDoc = (v: unknown): v is MemoryDoc => typeof v === 'object' && v !== null && typeof (v as MemoryDoc).id === 'number' && typeof (v as MemoryDoc).version === 'number'

// The library's shared memory: rules, learned preferences and working context.
export const useMemory = defineStore('memory', () => {
  const docs = ref<MemoryDoc[]>([])
  const layer = ref<LayerFilter>('rule')
  const status = ref<StatusFilter>('all')
  const loading = ref(false)
  const error = ref('')
  const next = ref<number | null>(null)
  let seq = 0

  async function load() {
    const n = ++seq
    loading.value = true
    error.value = ''
    try {
      const p = new URLSearchParams({ layer: layer.value, status: status.value, limit: '100' })
      const answer = await get<unknown>(`/api/inbox/memory?${p}`)
      if (n === seq) {
        docs.value = listOf<MemoryDoc>(answer, 'memory')
        next.value = (answer as { next?: number | null })?.next ?? null
      }
    } catch (err) {
      if (n === seq) error.value = errorText(err)
    } finally {
      if (n === seq) loading.value = false
    }
  }

  async function more() {
    if (next.value === null || loading.value) return
    const n = seq
    loading.value = true
    try {
      const p = new URLSearchParams({ layer: layer.value, status: status.value, limit: '100', after: String(next.value) })
      const answer = await get<{ memory: MemoryDoc[]; next?: number | null }>(`/api/inbox/memory?${p}`)
      if (n !== seq) return
      docs.value = [...docs.value, ...answer.memory]
      next.value = answer.next ?? null
    } catch (err) {
      if (n === seq) error.value = errorText(err)
    } finally {
      if (n === seq) loading.value = false
    }
  }

  function show(l: LayerFilter, s: StatusFilter = status.value) {
    layer.value = l
    status.value = s
    void load()
  }

  function replace(doc: MemoryDoc) {
    docs.value = docs.value.map((d) => (d.id === doc.id ? doc : d))
  }

  // write runs one change, and on a version conflict shows the current document while the form keeps its text.
  async function write(fn: () => Promise<unknown>, doc?: MemoryDoc): Promise<Result> {
    try {
      const answer = await fn()
      if (isDoc(answer) && doc) replace(answer)
      else await load()
      return { ok: true }
    } catch (err) {
      const body = err instanceof ApiError ? (err.body as Record<string, unknown> | undefined) : undefined
      if (errorCode(body) === 'stale_memory') {
        const now = [body?.document, body?.current, body?.memory].find(isDoc)
        if (now) replace(now)
        else await load()
        return { ok: false, stale: true, error: 'Someone changed this while you edited. It shows the saved version now; your text is kept, so save again to replace it.' }
      }
      return { ok: false, error: errorText(err) }
    }
  }

  const create = (r: { key: string; title: string; scope: string; body: string }) =>
    write(() => post<MemoryDoc>('/api/inbox/memory', { ...r, layer: 'rule' }))

  const correct = (doc: MemoryDoc, fields: Omit<MemoryCorrection, 'expected_version'>) =>
    write(() => put<MemoryDoc>(`/api/inbox/memory/${doc.id}`, { expected_version: doc.version, ...fields }), doc)

  const confirm = (doc: MemoryDoc) => write(() => post<MemoryDoc>(`/api/inbox/memory/${doc.id}/confirm`, { expected_version: doc.version }), doc)

  const remove = (doc: MemoryDoc) => write(() => del<unknown>(`/api/inbox/memory/${doc.id}?expected_version=${doc.version}`))

  const reset = (layers: MemoryLayer[]) => write(() => post<unknown>('/api/inbox/memory/reset', { layers }))

  return { docs, layer, status, loading, error, next, load, more, show, create, correct, confirm, remove, reset }
})
