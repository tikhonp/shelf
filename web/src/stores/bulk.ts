import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { ApiError, errorText, get, post } from '../api/client'
import type { RunPreview, RunProgress, RunSelection, SaveRefusal } from '../api/types'
import { ended } from '../lib/bulk'
import { useEditor } from './editor'
import { useLibrary } from './library'

// The albums picked for a bulk run, the fixes chosen, and the run itself.
export const useBulk = defineStore('bulk', () => {
  const selecting = ref(false)
  const panel = ref(false)
  const picked = ref(new Set<number>())
  // all picks what the list's query finds, most of which the list hasn't loaded.
  const all = ref(false)
  const except = ref(new Set<number>())
  const fixes = ref<string[]>([])
  const preview = ref<RunPreview | null>(null)
  const previewing = ref(false)
  const error = ref('')
  const run = ref<RunProgress | null>(null)
  const starting = ref(false)
  let asked = 0

  const library = () => useLibrary()
  const count = computed(() => (all.value ? library().total - except.value.size : picked.value.size))
  const working = computed(() => !!run.value && !ended(run.value))

  const has = (id: number) => (all.value ? !except.value.has(id) : picked.value.has(id))

  function changed() {
    panel.value = count.value > 0 || !!run.value
    void load()
  }

  function toggle(id: number) {
    const set = new Set(all.value ? except.value : picked.value)
    if (!set.delete(id)) set.add(id)
    if (all.value) except.value = set
    else picked.value = set
    changed()
  }

  function toggleAll() {
    all.value = !all.value
    picked.value = new Set()
    except.value = new Set()
    changed()
  }

  function clear() {
    picked.value = new Set()
    except.value = new Set()
    all.value = false
    fixes.value = []
    preview.value = null
    error.value = ''
  }

  function toggleSelecting() {
    selecting.value = !selecting.value
    if (selecting.value) {
      panel.value = count.value > 0 || !!run.value
      return
    }
    clear()
    if (!run.value) panel.value = false
  }

  function selection(): RunSelection {
    if (!all.value) return { albums: [...picked.value] }
    const l = library()
    return { all: true, q: l.query, filter: l.filter, folder: l.folder, source: l.source, except: [...except.value] }
  }

  function toggleFix(fix: string) {
    fixes.value = fixes.value.includes(fix) ? fixes.value.filter((f) => f !== fix) : [...fixes.value, fix]
    void load()
  }

  // load asks for the preview. Only the last answer counts, because a slow one
  // would show a choice the owner has since changed.
  async function load() {
    const mine = ++asked
    if (count.value === 0) {
      preview.value = null
      previewing.value = false
      return
    }
    previewing.value = true
    error.value = ''
    try {
      const next = await post<RunPreview>('/api/library/runs/preview', { selection: selection(), fixes: fixes.value })
      if (mine === asked) preview.value = next
    } catch (err) {
      if (mine === asked) error.value = errorText(err)
    } finally {
      if (mine === asked) previewing.value = false
    }
  }

  async function start() {
    starting.value = true
    error.value = ''
    try {
      run.value = await post<RunProgress>('/api/library/runs', { selection: selection(), fixes: fixes.value })
    } catch (err) {
      const refusal = err instanceof ApiError ? (err.body as SaveRefusal | undefined) : undefined
      error.value = refusal?.error ?? errorText(err)
    } finally {
      starting.value = false
    }
  }

  async function cancel() {
    if (!run.value) return
    try {
      applyRun(await post<RunProgress>(`/api/library/runs/${run.value.id}/cancel`))
    } catch (err) {
      error.value = errorText(err)
    }
  }

  // applyRun follows a run. An undo of one works from History, so only the
  // lamp shows it and nothing stays once it ends.
  function applyRun(r: RunProgress) {
    if (run.value?.id === r.id || !run.value || ended(run.value)) {
      const was = run.value
      run.value = r.kind === 'save' || !ended(r) ? r : null
      if (ended(r) && (!was || !ended(was))) void settled()
    }
  }

  // The files changed under the list and the open album, and staged edits stay.
  async function settled() {
    await Promise.allSettled([library().load(), library().loadFilters(), useEditor().reload()])
  }

  // check asks for the run at a start or a reconnect, because the stream has no
  // replay. A run that ended in between is read by its ID.
  async function check() {
    const was = run.value
    const { run: now } = await get<{ run: RunProgress | null }>('/api/library/runs/current')
    if (now) return applyRun(now)
    if (was && !ended(was)) applyRun(await get<RunProgress>(`/api/library/runs/${was.id}`))
  }

  // close leaves a finished run, with its selection.
  function close() {
    panel.value = false
    if (run.value && ended(run.value)) {
      run.value = null
      selecting.value = false
      clear()
    }
  }

  return {
    selecting, panel, picked, all, except, fixes, preview, previewing, error, run, starting, count, working,
    has, toggle, toggleAll, toggleSelecting, toggleFix, load, start, cancel, applyRun, check, close,
  }
})
