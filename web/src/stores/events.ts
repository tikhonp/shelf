import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { DownloadEvent, ScanEvent, SourceEvent } from '../api/types'
import { useDownloads } from './downloads'
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
    return Promise.allSettled([downloads.load(), status.check()])
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
