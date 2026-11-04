import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { ApiError, get, post } from '../api/client'
import type { ScanEvent, ScanState, Telegram, Wrapper, WrapperLogLine } from '../api/types'
import { useSources } from './sources'

// The parts beside the sources that the Status page and the shell show.
export const useStatus = defineStore('status', () => {
  const scan = ref<ScanState | null>(null)
  const telegram = ref<Telegram | null>(null)
  const wrapper = ref<Wrapper | null>(null)
  // wrapperAnswers is false while the wrapper's container doesn't answer.
  const wrapperAnswers = ref(true)
  const checkedAt = ref(0)

  async function loadScan() {
    scan.value = await get<ScanState>('/api/library/scan')
  }

  async function loadTelegram() {
    telegram.value = await get<Telegram>('/api/telegram')
  }

  async function loadWrapper() {
    try {
      wrapper.value = await get<Wrapper>('/api/applemusic/wrapper')
      wrapperAnswers.value = true
    } catch (err) {
      wrapper.value = null
      wrapperAnswers.value = !(err instanceof ApiError && err.status === 503)
    }
  }

  // check reloads every part. A part that fails keeps what it showed.
  async function check() {
    const sources = useSources()
    // Whether to read the wrapper depends on the sources, so they go first.
    const others = Promise.allSettled([loadScan(), loadTelegram()])
    await sources.load().catch(() => {})
    if (sources.byId('applemusic')?.enabled) await loadWrapper()
    await others
    checkedAt.value = Date.now()
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
    const sources = useSources()
    const out = sources.failing.map((s) => s.health.message || `${s.name} doesn't work`)
    if (sources.noneReady && out.length === 0) out.push('No source is ready')
    if (scanFails.value) out.push("Navidrome doesn't answer")
    if (telegramFails.value) out.push("The Telegram bot isn't connected")
    return out
  })

  return {
    scan,
    telegram,
    wrapper,
    wrapperAnswers,
    checkedAt,
    loadScan,
    loadTelegram,
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
