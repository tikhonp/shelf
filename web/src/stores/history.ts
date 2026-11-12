import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError, del, errorText, get, post } from '../api/client'
import type { ChangeConflict, LibraryChange, SaveRefusal } from '../api/types'
import { useEditor } from './editor'

interface Page {
  changes: LibraryChange[]
  next: number | null
}

// The timeline of Saves, with undo and redo in any order. It lists the whole
// library, or one album's changes while album is set.
export const useHistory = defineStore('history', () => {
  const changes = ref<LibraryChange[]>([])
  const next = ref<number | null>(null)
  const loading = ref(false)
  const error = ref('')
  const details = ref<Record<number, LibraryChange>>({})
  // running maps an undo or redo to the Save it works on.
  const running = ref<Record<number, number>>({})
  const conflicts = ref<Record<number, ChangeConflict[]>>({})
  const album = ref<number | null>(null)

  const albumParam = () => (album.value === null ? '' : `album=${album.value}`)
  const url = (path: string, ...params: string[]) => {
    const q = [...params, albumParam()].filter(Boolean).join('&')
    return q ? `${path}?${q}` : path
  }

  // load reads the first page. A new scope drops the old rows and details at
  // once. A run opened from an album lists only that album's files, so its
  // details differ by scope.
  async function load(scope?: number | null) {
    if (scope !== undefined && scope !== album.value) {
      album.value = scope
      changes.value = []
      next.value = null
      details.value = {}
    }
    const asked = album.value
    loading.value = true
    error.value = ''
    try {
      const page = await get<Page>(url('/api/library/history'))
      if (asked !== album.value) return
      changes.value = page.changes
      next.value = page.next
    } catch (err) {
      if (asked === album.value) error.value = errorText(err)
    } finally {
      if (asked === album.value) loading.value = false
    }
  }

  async function more() {
    if (next.value === null) return
    const asked = album.value
    const page = await get<Page>(url('/api/library/history', `before=${next.value}`))
    if (asked !== album.value) return
    changes.value = [...changes.value, ...page.changes]
    next.value = page.next
  }

  async function open(id: number) {
    const asked = album.value
    const c = await get<LibraryChange>(url(`/api/library/changes/${id}`))
    if (asked === album.value) details.value = { ...details.value, [id]: c }
  }

  // reverse asks first, as a Save does, when putting tracks back would split
  // or merge albums or rename an album artist in Navidrome.
  async function reverse(c: LibraryChange, confirm: string[] = []) {
    error.value = ''
    const { [c.id]: _, ...rest } = conflicts.value
    conflicts.value = rest
    const editor = useEditor()
    try {
      const job = await post<LibraryChange>(`/api/library/changes/${c.id}/${c.undone ? 'redo' : 'undo'}`, confirm.length > 0 ? { confirm } : undefined)
      running.value = { ...running.value, [job.id]: c.id }
      if (confirm.length > 0) editor.cancelAsks()
    } catch (err) {
      const refusal = err instanceof ApiError ? (err.body as SaveRefusal | undefined) : undefined
      if (refusal?.reason === 'confirm') {
        editor.ask(refusal.confirm ?? [], (next) => reverse(c, next), c.undone ? 'Redo anyway' : 'Undo anyway')
        return
      }
      if (confirm.length > 0) editor.cancelAsks()
      error.value = refusal?.error ?? errorText(err)
    }
  }

  // applyChange follows an undo or redo. The album under the editor changes with
  // it, and staged edits go out with the new revisions.
  function applyChange(c: LibraryChange) {
    const save = running.value[c.id]
    if (c.state === 'queued' || c.state === 'writing') return
    // The list also reloads when a run nobody here started ends, when a delete
    // ends, and when a Save it shows as writing ends.
    if (save === undefined) {
      if (c.run || c.kind === 'delete' || changes.value.some((x) => x.id === c.id)) void load()
      return
    }
    const { [c.id]: _, ...rest } = running.value
    running.value = rest
    if (c.conflicts.length > 0) conflicts.value = { ...conflicts.value, [save]: c.conflicts }
    if (c.state === 'failed') error.value = c.error
    void load()
    void useEditor().afterReverse(c)
  }

  async function clear() {
    await del('/api/library/history')
    changes.value = []
    next.value = null
    details.value = {}
  }

  return { changes, next, loading, error, details, running, conflicts, album, load, more, open, reverse, applyChange, clear }
})
