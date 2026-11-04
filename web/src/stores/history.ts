import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError, del, errorText, get, post } from '../api/client'
import type { ChangeConflict, LibraryChange, SaveRefusal } from '../api/types'
import { useEditor } from './editor'

interface Page {
  changes: LibraryChange[]
  next: number | null
}

// The timeline of Saves, with undo and redo in any order.
export const useHistory = defineStore('history', () => {
  const changes = ref<LibraryChange[]>([])
  const next = ref<number | null>(null)
  const loading = ref(false)
  const error = ref('')
  const details = ref<Record<number, LibraryChange>>({})
  // running maps an undo or redo to the Save it works on.
  const running = ref<Record<number, number>>({})
  const conflicts = ref<Record<number, ChangeConflict[]>>({})

  async function load() {
    loading.value = true
    error.value = ''
    try {
      const page = await get<Page>('/api/library/history')
      changes.value = page.changes
      next.value = page.next
    } catch (err) {
      error.value = errorText(err)
    } finally {
      loading.value = false
    }
  }

  async function more() {
    if (next.value === null) return
    const page = await get<Page>(`/api/library/history?before=${next.value}`)
    changes.value = [...changes.value, ...page.changes]
    next.value = page.next
  }

  async function open(id: number) {
    details.value = { ...details.value, [id]: await get<LibraryChange>(`/api/library/changes/${id}`) }
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
    // A run nobody here started ends in the list too.
    if (save === undefined) {
      if (c.run) void load()
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

  return { changes, next, loading, error, details, running, conflicts, load, more, open, reverse, applyChange, clear }
})
