import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { ApiError, get, post } from '../api/client'
import type { MusicBrainzState, ScanEvent, ScanState, Telegram, Wrapper, WrapperLogLine } from '../api/types'
import { useDownloads } from './downloads'
import { useLibrary } from './library'
import { useSources } from './sources'

// The parts beside the sources that the Status page and the shell show.
export const useStatus = defineStore('status', () => {
  const scan = ref<ScanState | null>(null)
  const telegram = ref<Telegram | null>(null)
  const musicbrainz = ref<MusicBrainzState | null>(null)
  const wrapper = ref<Wrapper | null>(null)
  // wrapperAnswers is false while the wrapper's container doesn't answer.
  const wrapperAnswers = ref(true)
  const checkedAt = ref(0)
  let checkSeq = 0
  let telegramSeq = 0
  let wrapperSeq = 0

  function clearDownloadParts() {
    telegramSeq++
    wrapperSeq++
    useSources().clear()
    telegram.value = null
    wrapper.value = null
    wrapperAnswers.value = true
  }

  async function loadScan() {
    scan.value = await get<ScanState>('/api/library/scan')
  }

  async function loadTelegram() {
    const seq = ++telegramSeq
    const downloads = useDownloads()
    const gen = downloads.loadGeneration()
    try {
      const next = await get<Telegram>('/api/telegram')
      if (seq !== telegramSeq || downloads.loadGeneration() !== gen || downloads.enabled === false) return
      telegram.value = next
    } catch (err) {
      if (seq !== telegramSeq || downloads.loadGeneration() !== gen || downloads.enabled === false) return
      if (err instanceof ApiError && err.status === 404) {
        telegram.value = null
        return
      }
      throw err
    }
  }

  async function loadMusicBrainz() {
    musicbrainz.value = await get<MusicBrainzState>('/api/library/musicbrainz')
  }

  async function loadWrapper() {
    const seq = ++wrapperSeq
    const downloads = useDownloads()
    const gen = downloads.loadGeneration()
    try {
      const next = await get<Wrapper>('/api/applemusic/wrapper')
      if (seq !== wrapperSeq || downloads.loadGeneration() !== gen || downloads.enabled === false) return
      wrapper.value = next
      wrapperAnswers.value = true
    } catch (err) {
      if (seq !== wrapperSeq || downloads.loadGeneration() !== gen || downloads.enabled === false) return
      wrapper.value = null
      wrapperAnswers.value = !(err instanceof ApiError && err.status === 503)
    }
  }

  // check reloads every part. A part that fails keeps what it showed.
  async function check(opts?: { retryDetect?: boolean }) {
    const seq = ++checkSeq
    const sources = useSources()
    const downloads = useDownloads()
    const base = Promise.allSettled([loadScan(), loadMusicBrainz(), useLibrary().loadIndex()])
    const retryDetect = opts?.retryDetect ?? true
    if (downloads.enabled === null || (retryDetect && downloads.detectError)) {
      await downloads.detect()
    }
    if (seq !== checkSeq) {
      await base
      return
    }
    if (downloads.enabled === false) {
      clearDownloadParts()
    } else if (downloads.enabled === true) {
      const gen = downloads.loadGeneration()
      let downloadsOff = false
      await sources.load().catch((err) => {
        if (err instanceof ApiError && err.status === 404) {
          downloadsOff = true
          if (seq === checkSeq && downloads.markDisabled(gen)) {
            clearDownloadParts()
          }
        }
      })
      if (!downloadsOff && seq === checkSeq && downloads.loadGeneration() === gen) {
        const extra: Promise<unknown>[] = [loadTelegram()]
        if (sources.byId('applemusic')?.enabled) extra.push(loadWrapper())
        await Promise.allSettled(extra)
      }
    }
    await base
    if (seq === checkSeq) checkedAt.value = Date.now()
  }

  function applyScan(e: ScanEvent) {
    if (scan.value) scan.value = { ...scan.value, ...e }
  }

  function applyWrapper(w: Wrapper | null | undefined) {
    if (w) wrapper.value = w
  }

  async function startScan() {
    await post('/api/library/scan')
    if (scan.value) scan.value = { ...scan.value, scanning: true }
  }

  const login = (username: string, password: string) => post<Wrapper>('/api/applemusic/wrapper/login', { username, password })
  const sendCode = (code: string) => post<Wrapper>('/api/applemusic/wrapper/code', { code })
  const restart = () => post<Wrapper>('/api/applemusic/wrapper/restart')
  const wrapperLog = (after: number) => get<{ lines: WrapperLogLine[] }>(`/api/applemusic/wrapper/log?after=${after}`)

  const scanFails = computed(() => !!scan.value?.enabled && !scan.value.ok)
  const telegramFails = computed(() => !!telegram.value?.enabled && !telegram.value.connected)

  // attention names what the Status tab's lamp is lit for, and nothing when all works.
  const attention = computed(() => {
    const out: string[] = []
    if (useDownloads().enabled) {
      const sources = useSources()
      out.push(...sources.failing.map((s) => s.health.message || `${s.name} doesn't work`))
      if (sources.noneReady && out.length === 0) out.push('No source is ready')
      if (telegramFails.value) out.push("The Telegram bot isn't connected")
    }
    if (scanFails.value) out.push("Navidrome doesn't answer")
    return out
  })

  return {
    scan,
    telegram,
    musicbrainz,
    wrapper,
    wrapperAnswers,
    checkedAt,
    loadScan,
    loadTelegram,
    loadMusicBrainz,
    loadWrapper,
    check,
    applyScan,
    applyWrapper,
    startScan,
    login,
    sendCode,
    restart,
    wrapperLog,
    scanFails,
    telegramFails,
    attention,
  }
})
