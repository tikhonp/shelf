import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { DownloadEvent, LibraryChange, LibraryIndex, RunProgress, ScanEvent, SourceEvent } from '../api/types'
import { useBulk } from './bulk'
import { useDownloads } from './downloads'
import { useEditor } from './editor'
import { useHistory } from './history'
import { useLibrary } from './library'
import { useSources } from './sources'
import { useStatus } from './status'

export const useEvents = defineStore('events', () => {
  const connected = ref(false)
  let stream: EventSource | null = null
  let opened = false

  // reload reads again what the page shows, because the stream has no replay.
  function reload() {
    const downloads = useDownloads()
    const status = useStatus()
    return Promise.allSettled([downloads.load(), status.check(), useEditor().recheck(), useBulk().check()])
  }

  // A page kept for the back button would hold its stream, and browsers allow
  // six connections to a host, so a few reloads would hang the next one.
  function leave() {
    stream?.close()
    stream = null
  }
  function back(e: PageTransitionEvent) {
    if (e.persisted) connect()
  }

  function connect() {
    if (stream) return
    window.addEventListener('pagehide', leave)
    window.addEventListener('pageshow', back)
    stream = new EventSource('/api/events')
    stream.onopen = () => {
      connected.value = true
      if (opened) void reload()
      opened = true
    }
    stream.onerror = () => {
      connected.value = false
    }
    stream.addEventListener('download', (m) => useDownloads().applyEvent(read<DownloadEvent>(m)))
    stream.addEventListener('scan', (m) => useStatus().applyScan(read<ScanEvent>(m)))
    stream.addEventListener('library', (m) => {
      if (useLibrary().applyIndex(read<LibraryIndex>(m))) void useEditor().follow()
    })
    stream.addEventListener('change', (m) => {
      const c = read<LibraryChange>(m)
      useEditor().applyChange(c)
      useHistory().applyChange(c)
    })
    stream.addEventListener('run', (m) => useBulk().applyRun(read<RunProgress>(m)))
    stream.addEventListener('suggestions', (m) => useEditor().suggestionsChanged(read<{ album: number }>(m).album))
    stream.addEventListener('source', (m) => {
      const e = read<SourceEvent>(m)
      useSources().applyEvent(e)
      useStatus().applyWrapper(e.wrapper)
    })
  }

  function disconnect() {
    window.removeEventListener('pagehide', leave)
    window.removeEventListener('pageshow', back)
    leave()
    opened = false
  }

  return { connected, connect, disconnect }
})

function read<T>(m: Event): T {
  return JSON.parse((m as MessageEvent<string>).data) as T
}
